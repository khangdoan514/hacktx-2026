package productapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/agents/contracts"
	"github.com/khangdoan514/hacktx-2026/internal/agents/runtime"
	"github.com/khangdoan514/hacktx-2026/internal/approvals"
	"github.com/khangdoan514/hacktx-2026/internal/capabilities"
	"github.com/khangdoan514/hacktx-2026/internal/documents"
	"github.com/khangdoan514/hacktx-2026/internal/notifications"
	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
	"github.com/khangdoan514/hacktx-2026/internal/requests"
	"github.com/khangdoan514/hacktx-2026/internal/resources"
)

type Server struct {
	DB     *platform.DB
	Config platform.Config
	Engine *runtime.Engine
	Store  *runtime.Store
	Hub    *notifications.Hub
	Keys   *platform.JWKS
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/ready", s.ready)
	mux.HandleFunc("POST /api/v1/auth/local-session", s.localSession)
	mux.HandleFunc("GET /api/v1/me", s.authed(func(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
		profile, err := organization.Me(ctx, tx, actor)
		return http.StatusOK, profile, err
	}))
	mux.HandleFunc("GET /api/v1/organization", s.authed(func(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
		body, err := organization.Directory(ctx, tx)
		return http.StatusOK, body, err
	}))
	mux.HandleFunc("GET /api/v1/notifications", s.authed(func(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
		items, err := notifications.List(ctx, tx, actor)
		return http.StatusOK, map[string]any{"items": items}, err
	}))
	mux.HandleFunc("GET /api/v1/events/stream", s.stream)
	mux.HandleFunc("POST /api/v1/invitations", s.authed(s.invite))
	mux.HandleFunc("POST /api/v1/invitations/accept", s.acceptInvite)
	mux.HandleFunc("POST /api/v1/members/{id}/remove", s.authed(s.removeMember))
	mux.HandleFunc("GET /api/v1/requests", s.authed(s.listRequests))
	mux.HandleFunc("POST /api/v1/requests", s.authed(s.createRequest))
	mux.HandleFunc("GET /api/v1/requests/{id}", s.authed(s.getRequest))
	mux.HandleFunc("POST /api/v1/requests/{id}/save", s.authed(s.saveRequest))
	mux.HandleFunc("POST /api/v1/requests/{id}/submit", s.authed(s.withRequest(requests.Submit)))
	mux.HandleFunc("POST /api/v1/requests/{id}/triage", s.authed(s.withRequest(requests.Triage)))
	mux.HandleFunc("POST /api/v1/requests/{id}/accept", s.authed(s.withRequest(requests.Accept)))
	mux.HandleFunc("POST /api/v1/requests/{id}/reject", s.authed(s.withRequest(requests.Reject)))
	mux.HandleFunc("POST /api/v1/requests/{id}/clarify", s.authed(s.clarify))
	mux.HandleFunc("POST /api/v1/requests/{id}/answer", s.authed(s.answer))
	mux.HandleFunc("POST /api/v1/requests/{id}/cancel", s.authed(s.cancelRequest))
	mux.HandleFunc("POST /api/v1/requests/{id}/reopen", s.authed(s.reopenRequest))
	mux.HandleFunc("POST /api/v1/requests/{id}/start-delivery", s.authed(s.withRequest(requests.StartDelivery)))
	mux.HandleFunc("POST /api/v1/requests/{id}/ready", s.authed(s.withRequest(requests.MarkReady)))
	mux.HandleFunc("POST /api/v1/requests/{id}/fulfill", s.authed(s.withRequest(requests.Fulfill)))
	mux.HandleFunc("POST /api/v1/requests/{id}/tasks", s.authed(s.addTask))
	mux.HandleFunc("POST /api/v1/tasks/{id}/complete", s.authed(s.completeTask))
	mux.HandleFunc("POST /api/v1/requests/{id}/specifications", s.authed(s.createSpec))
	mux.HandleFunc("POST /api/v1/specifications/{id}/revisions", s.authed(s.reviseSpec))
	mux.HandleFunc("POST /api/v1/specifications/{id}/submit-review", s.authed(s.submitSpec))
	mux.HandleFunc("POST /api/v1/requests/{id}/approvals", s.authed(s.createApproval))
	mux.HandleFunc("POST /api/v1/approvals/{id}/decide", s.authed(s.decide))
	mux.HandleFunc("POST /api/v1/requests/{id}/handovers", s.authed(s.createHandover))
	mux.HandleFunc("POST /api/v1/handovers/{id}/send", s.authed(s.sendHandover))
	mux.HandleFunc("POST /api/v1/handovers/{id}/acknowledge", s.authed(s.ackHandover))
	mux.HandleFunc("POST /api/v1/handovers/{id}/complete", s.authed(s.completeHandover))
	mux.HandleFunc("POST /api/v1/handovers/{id}/return", s.authed(s.returnHandover))
	mux.HandleFunc("POST /api/v1/handovers/{id}/cancel", s.authed(s.cancelHandover))
	mux.HandleFunc("GET /api/v1/handovers/{id}", s.authed(s.getHandover))
	mux.HandleFunc("POST /api/v1/specifications/{id}/catalog", s.authed(s.openCatalog))
	mux.HandleFunc("POST /api/v1/catalog/{id}/test", s.authed(s.testCatalog))
	mux.HandleFunc("POST /api/v1/catalog/{id}/evaluations", s.authed(s.evaluate))
	mux.HandleFunc("POST /api/v1/catalog/{id}/publish", s.authed(s.publish))
	mux.HandleFunc("POST /api/v1/catalog/{id}/suspend", s.authed(s.suspend))
	mux.HandleFunc("POST /api/v1/catalog/{id}/retire", s.authed(s.retire))
	mux.HandleFunc("GET /api/v1/catalog/{id}", s.authed(s.getCatalog))
	mux.HandleFunc("GET /api/v1/requests/{id}/resources", s.authed(s.listResources))
	mux.HandleFunc("POST /api/v1/requests/{id}/resources", s.authed(s.createResource))
	mux.HandleFunc("POST /api/v1/resources/{id}/approve", s.authed(s.approveResource))
	mux.HandleFunc("POST /api/v1/requests/{id}/documents", s.authed(s.initiateFile))
	mux.HandleFunc("PUT /api/v1/documents/{id}/bytes", s.authed(s.uploadFile))
	mux.HandleFunc("POST /api/v1/documents/{id}/finalize", s.authed(s.finalizeFile))
	mux.HandleFunc("GET /api/v1/documents/{id}", s.authed(s.downloadFile))
	mux.HandleFunc("POST /v1/agent-runs", s.authed(s.startRun))
	mux.HandleFunc("GET /v1/agent-runs/{id}", s.authed(s.getRun))
	mux.HandleFunc("GET /v1/agent-runs/{id}/events", s.authed(s.listEvents))
	mux.HandleFunc("POST /v1/agent-runs/{id}/cancel", s.authed(s.cancelRun))
	mux.HandleFunc("GET /v1/agents", s.authed(s.listAgents))
	return mux
}

type command func(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (requests.Record, error)
type bodyDoc struct {
	ExpectedVersion int             `json:"expected_version"`
	Title           string          `json:"title"`
	Outcome         string          `json:"outcome"`
	Reason          string          `json:"reason"`
	Question        string          `json:"question"`
	Answer          string          `json:"answer"`
	ClarificationID string          `json:"clarification_id"`
	ReviewerID      string          `json:"reviewer_id"`
	Comment         string          `json:"comment"`
	Decision        string          `json:"decision"`
	Document        json.RawMessage `json:"document"`
	Evidence        string          `json:"evidence"`
	Passed          bool            `json:"passed"`
	Kind            string          `json:"kind"`
	Unit            string          `json:"unit"`
	Amount          float64         `json:"amount"`
	Email           string          `json:"email"`
	TeamID          string          `json:"team_id"`
	Role            string          `json:"role"`
	Token           string          `json:"token"`
	Filename        string          `json:"filename"`
	Checksum        string          `json:"checksum"`
	Note            string          `json:"note"`
	TargetVersion   int             `json:"target_version"`
}

func (s *Server) authed(fn func(http.ResponseWriter, *http.Request, context.Context, pgx.Tx, platform.Actor) (int, any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := platform.Bearer(r.Header.Get("Authorization"))
		if err != nil {
			writeErr(w, err)
			return
		}
		identity, err := platform.Verify(s.Config, token, s.Keys)
		if err != nil {
			writeErr(w, err)
			return
		}
		var status int
		var body any
		err = s.DB.WithUser(r.Context(), identity.UserID, identity.Email, func(ctx context.Context, tx pgx.Tx) error {
			actor, err := organization.Load(ctx, tx, identity.UserID, identity.Email)
			if err != nil {
				return err
			}
			status, body, err = fn(w, r, platform.WithActor(ctx, actor), tx, actor)
			return err
		})
		if err != nil {
			writeErr(w, err)
			return
		}
		if status == 0 {
			return
		}
		writeJSON(w, status, body)
	}
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.Pool.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) localSession(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decode(r, &body); err != nil {
		writeErr(w, platform.Fail(http.StatusBadRequest, "validation", "Invalid JSON"))
		return
	}
	token, err := organization.Login(r.Context(), s.DB, s.Config, body.Email, body.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"access_token": token, "token_type": "bearer"})
}

func (s *Server) withRequest(fn command) func(http.ResponseWriter, *http.Request, context.Context, pgx.Tx, platform.Actor) (int, any, error) {
	return func(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
		var body bodyDoc
		_ = decode(r, &body)
		record, err := fn(ctx, tx, actor, r.PathValue("id"), body.ExpectedVersion)
		return http.StatusOK, record, err
	}
}

func (s *Server) listRequests(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	items, err := requests.List(ctx, tx)
	return http.StatusOK, map[string]any{"items": items}, err
}

func (s *Server) createRequest(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	if err := decode(r, &body); err != nil {
		return 0, nil, platform.Fail(http.StatusBadRequest, "validation", "Invalid JSON")
	}
	record, err := requests.Create(ctx, tx, actor, body.Title, body.Outcome)
	return http.StatusCreated, record, err
}

func (s *Server) saveRequest(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	if err := decode(r, &body); err != nil {
		return 0, nil, err
	}
	record, err := requests.SaveDraft(ctx, tx, actor, r.PathValue("id"), body.Title, body.Outcome, body.ExpectedVersion)
	return http.StatusOK, record, err
}

func (s *Server) getRequest(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	record, err := requests.Get(ctx, tx, r.PathValue("id"))
	if err != nil {
		return 0, nil, err
	}
	clarifications, err := requests.Clarifications(ctx, tx, record.ID)
	if err != nil {
		return 0, nil, err
	}
	spec, err := capabilities.SpecForRequest(ctx, tx, record.ID)
	if err != nil {
		return 0, nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id::text, state, version FROM app.capability_handovers WHERE request_id = $1 ORDER BY created_at`, record.ID)
	if err != nil {
		return 0, nil, err
	}
	handovers := []map[string]any{}
	for rows.Next() {
		var id, state string
		var version int
		if err := rows.Scan(&id, &state, &version); err != nil {
			return 0, nil, err
		}
		handovers = append(handovers, map[string]any{"id": id, "state": state, "version": version})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	resourceItems, err := resources.List(ctx, tx, record.ID)
	if err != nil {
		return 0, nil, err
	}
	approvalRows, err := tx.Query(ctx, `
		SELECT id::text, state, version, reviewer_id::text
		FROM app.scoped_approvals WHERE scope_id = $1 ORDER BY created_at
	`, record.ID)
	if err != nil {
		return 0, nil, err
	}
	defer approvalRows.Close()
	approvalItems := []map[string]any{}
	for approvalRows.Next() {
		var approvalID, approvalState, reviewerID string
		var approvalVersion int
		if err := approvalRows.Scan(&approvalID, &approvalState, &approvalVersion, &reviewerID); err != nil {
			return 0, nil, err
		}
		approvalItems = append(approvalItems, map[string]any{"id": approvalID, "state": approvalState, "version": approvalVersion, "reviewer_id": reviewerID})
	}
	if err := approvalRows.Err(); err != nil {
		return 0, nil, err
	}
	var catalog any
	if spec.ID != "" {
		var catalogID, catalogState string
		var catalogVersion int
		err = tx.QueryRow(ctx, `
			SELECT id::text, state, version FROM app.catalog_releases
			WHERE specification_id = $1 ORDER BY created_at DESC LIMIT 1
		`, spec.ID).Scan(&catalogID, &catalogState, &catalogVersion)
		if err == nil {
			catalog = map[string]any{"id": catalogID, "state": catalogState, "version": catalogVersion}
		} else if !platform.Missing(err) {
			return 0, nil, err
		}
	}
	return http.StatusOK, map[string]any{"request": record, "clarifications": clarifications, "specification": spec, "handovers": handovers, "resources": resourceItems, "approvals": approvalItems, "catalog": catalog}, nil
}

func (s *Server) clarify(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	if err := decode(r, &body); err != nil {
		return 0, nil, err
	}
	record, item, err := requests.Clarify(ctx, tx, actor, r.PathValue("id"), body.Question, body.ExpectedVersion)
	if err != nil {
		return 0, nil, err
	}
	if err := notifications.Notify(ctx, tx, s.Hub, actor.OrgID, record.RequesterID, "clarification", "request", record.ID, body.Question); err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]any{"request": record, "clarification": item}, nil
}

func (s *Server) answer(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	if err := decode(r, &body); err != nil {
		return 0, nil, err
	}
	record, err := requests.Answer(ctx, tx, actor, r.PathValue("id"), body.ClarificationID, body.Answer, body.ExpectedVersion)
	return http.StatusOK, record, err
}

func (s *Server) cancelRequest(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	record, err := requests.Cancel(ctx, tx, actor, r.PathValue("id"), body.Reason, body.ExpectedVersion)
	return http.StatusOK, record, err
}

func (s *Server) reopenRequest(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	record, err := requests.Reopen(ctx, tx, actor, r.PathValue("id"), body.Reason, body.ExpectedVersion)
	return http.StatusOK, record, err
}

func (s *Server) addTask(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	id, err := requests.AddTask(ctx, tx, actor, r.PathValue("id"), body.Title)
	return http.StatusCreated, map[string]string{"id": id}, err
}

func (s *Server) completeTask(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	err := requests.CompleteTask(ctx, tx, actor, r.PathValue("id"), body.Evidence, body.ExpectedVersion)
	return http.StatusOK, map[string]string{"state": "done"}, err
}

func (s *Server) createSpec(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	if err := decode(r, &body); err != nil {
		return 0, nil, err
	}
	spec, err := capabilities.CreateSpec(ctx, tx, actor, r.PathValue("id"), body.Document)
	return http.StatusCreated, spec, err
}

func (s *Server) reviseSpec(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	if err := decode(r, &body); err != nil {
		return 0, nil, err
	}
	spec, err := capabilities.Revise(ctx, tx, actor, r.PathValue("id"), body.ExpectedVersion, body.Document)
	return http.StatusOK, spec, err
}

func (s *Server) submitSpec(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	spec, err := capabilities.SubmitReview(ctx, tx, actor, r.PathValue("id"), body.ExpectedVersion)
	return http.StatusOK, spec, err
}

func (s *Server) createApproval(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	if err := decode(r, &body); err != nil {
		return 0, nil, err
	}
	spec, err := capabilities.SpecForRequest(ctx, tx, r.PathValue("id"))
	if err != nil || spec.ID == "" {
		return 0, nil, platform.Fail(http.StatusNotFound, "not_found", "Specification not found")
	}
	decision, err := approvals.Create(ctx, tx, actor, r.PathValue("id"), spec.ID, body.ReviewerID, spec.MaterialRevision)
	return http.StatusCreated, decision, err
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return 0, nil, err
	}
	var body bodyDoc
	if err := json.Unmarshal(raw, &body); err != nil {
		return 0, nil, platform.Fail(http.StatusBadRequest, "validation", "Invalid JSON")
	}
	decision, status, err := approvals.Decide(ctx, tx, actor, r.PathValue("id"), body.Decision, body.Comment, r.Header.Get("Idempotency-Key"), body.ExpectedVersion, raw)
	return status, decision, err
}

func (s *Server) createHandover(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	item, err := requests.CreateHandover(ctx, tx, actor, r.PathValue("id"))
	return http.StatusCreated, item, err
}

func (s *Server) sendHandover(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := requests.SendHandover(ctx, tx, actor, r.PathValue("id"), body.ExpectedVersion)
	return http.StatusOK, item, err
}

func (s *Server) ackHandover(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := requests.AcknowledgeHandover(ctx, tx, actor, r.PathValue("id"), body.ExpectedVersion)
	return http.StatusOK, item, err
}

func (s *Server) completeHandover(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := requests.CompleteHandover(ctx, tx, actor, r.PathValue("id"), body.ExpectedVersion)
	return http.StatusOK, item, err
}

func (s *Server) returnHandover(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := requests.ReturnHandover(ctx, tx, actor, r.PathValue("id"), body.Note, body.ExpectedVersion)
	return http.StatusOK, item, err
}

func (s *Server) cancelHandover(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := requests.CancelHandover(ctx, tx, actor, r.PathValue("id"), body.ExpectedVersion)
	return http.StatusOK, item, err
}

func (s *Server) getHandover(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	item, err := requests.GetHandover(ctx, tx, r.PathValue("id"))
	return http.StatusOK, item, err
}

func (s *Server) openCatalog(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	item, err := capabilities.OpenCatalog(ctx, tx, actor, r.PathValue("id"))
	return http.StatusCreated, item, err
}

func (s *Server) testCatalog(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := capabilities.StartTesting(ctx, tx, actor, r.PathValue("id"), body.ExpectedVersion)
	return http.StatusOK, item, err
}

func (s *Server) evaluate(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := capabilities.RecordEvaluation(ctx, tx, actor, r.PathValue("id"), body.Evidence, body.Passed)
	return http.StatusOK, item, err
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := capabilities.Publish(ctx, tx, actor, r.PathValue("id"), body.ExpectedVersion)
	return http.StatusOK, item, err
}

func (s *Server) suspend(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := capabilities.Suspend(ctx, tx, actor, r.PathValue("id"), body.ExpectedVersion)
	return http.StatusOK, item, err
}

func (s *Server) retire(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := capabilities.Retire(ctx, tx, actor, r.PathValue("id"), body.ExpectedVersion)
	return http.StatusOK, item, err
}

func (s *Server) getCatalog(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	item, err := capabilities.GetCatalog(ctx, tx, r.PathValue("id"))
	return http.StatusOK, item, err
}

func (s *Server) listResources(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	items, err := resources.List(ctx, tx, r.PathValue("id"))
	return http.StatusOK, map[string]any{"items": items}, err
}

func (s *Server) createResource(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := resources.Create(ctx, tx, actor, r.PathValue("id"), body.Kind, body.Unit, body.Amount)
	return http.StatusCreated, item, err
}

func (s *Server) approveResource(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := resources.Approve(ctx, tx, actor, r.PathValue("id"), body.Amount)
	return http.StatusOK, item, err
}

func (s *Server) initiateFile(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := documents.Initiate(ctx, tx, actor, r.PathValue("id"), body.Filename)
	return http.StatusCreated, item, err
}

func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		return 0, nil, err
	}
	if err := documents.WriteBytes(s.Config.StorageDir, r.PathValue("id"), data); err != nil {
		return 0, nil, err
	}
	return http.StatusOK, map[string]string{"state": "uploaded"}, nil
}

func (s *Server) finalizeFile(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	_ = decode(r, &body)
	item, err := documents.Finalize(ctx, tx, actor, s.Config.StorageDir, r.PathValue("id"), body.Checksum)
	return http.StatusOK, item, err
}

func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	item, data, err := documents.Open(ctx, tx, s.Config.StorageDir, r.PathValue("id"))
	if err != nil {
		return 0, nil, err
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename="+item.Filename)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
	return 0, nil, nil
}

func (s *Server) invite(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var body bodyDoc
	if err := decode(r, &body); err != nil {
		return 0, nil, err
	}
	id, token, err := organization.Invite(ctx, tx, actor, body.Email, body.TeamID, body.Role)
	return http.StatusCreated, map[string]string{"id": id, "token": token}, err
}

func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request) {
	token, err := platform.Bearer(r.Header.Get("Authorization"))
	if err != nil {
		writeErr(w, err)
		return
	}
	identity, err := platform.Verify(s.Config, token, s.Keys)
	if err != nil {
		writeErr(w, err)
		return
	}
	var body bodyDoc
	if err := decode(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	err = s.DB.WithUser(r.Context(), identity.UserID, identity.Email, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT app.provision_profile($1, $2)`, identity.Email, identity.Email); err != nil {
			return err
		}
		return organization.Accept(ctx, tx, body.Token, identity.Email)
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "accepted"})
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	err := organization.RemoveMembership(ctx, tx, actor, r.PathValue("id"))
	return http.StatusOK, map[string]string{"status": "removed"}, err
}

func (s *Server) startRun(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	var request contracts.StartRunRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "invalid request JSON")
	}
	run, err := s.Engine.Start(ctx, request)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusAccepted, contracts.StartRunResponse{RunID: run.ID, State: run.State}, nil
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	run, err := s.Store.Get(ctx, r.PathValue("id"))
	if err != nil {
		return 0, nil, err
	}
	meta, _ := s.Store.LoadMeta(ctx, run.ID)
	return http.StatusOK, map[string]any{"run": run, "execution_mode": meta.ExecutionMode, "adapter_version": meta.AdapterVersion}, nil
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	after := uint64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return 0, nil, platform.Fail(http.StatusBadRequest, "invalid_input", "invalid event cursor")
		}
		after = parsed
	}
	if _, err := s.Store.Get(ctx, r.PathValue("id")); err != nil {
		return 0, nil, err
	}
	events, err := s.Store.List(ctx, r.PathValue("id"), after)
	return http.StatusOK, contracts.EventsResponse{RunID: r.PathValue("id"), Events: events}, err
}

func (s *Server) cancelRun(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	if err := s.Store.RequestCancel(ctx, r.PathValue("id")); err != nil {
		return 0, nil, err
	}
	return http.StatusAccepted, map[string]string{"status": "cancel_requested"}, nil
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx, actor platform.Actor) (int, any, error) {
	items := []contracts.AgentSummary{}
	for _, definition := range s.Engine.Definitions {
		items = append(items, contracts.AgentSummary{ID: definition.ID, Version: definition.Version, Name: definition.Name})
	}
	slices.SortFunc(items, func(a, b contracts.AgentSummary) int { return strings.Compare(a.ID, b.ID) })
	return http.StatusOK, contracts.AgentsResponse{Agents: items}, nil
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	token, err := platform.Bearer(r.Header.Get("Authorization"))
	if err != nil {
		writeErr(w, err)
		return
	}
	identity, err := platform.Verify(s.Config, token, s.Keys)
	if err != nil {
		writeErr(w, err)
		return
	}
	var actor platform.Actor
	err = s.DB.WithUser(r.Context(), identity.UserID, identity.Email, func(ctx context.Context, tx pgx.Tx) error {
		actor, err = organization.Load(ctx, tx, identity.UserID, identity.Email)
		return err
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, platform.Fail(http.StatusInternalServerError, "internal", "Streaming is not available"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	events, cancel := s.Hub.Subscribe(actor.ID)
	defer cancel()
	_, _ = w.Write([]byte(": ok\n\n"))
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case message := <-events:
			_, _ = w.Write([]byte("data: " + strings.ReplaceAll(message, "\n", " ") + "\n\n"))
			flusher.Flush()
		}
	}
}

func decode(r *http.Request, dest any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return platform.Fail(http.StatusBadRequest, "validation", "Invalid JSON")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, err error) {
	if runErr, ok := err.(*contracts.RunError); ok {
		status := http.StatusBadRequest
		if runErr.Code == contracts.ErrNotFound {
			status = http.StatusNotFound
		}
		if runErr.Code == contracts.ErrInternal {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, contracts.ErrorResponse{Error: *runErr})
		return
	}
	appErr := platform.As(err)
	if appErr == nil {
		appErr = platform.Fail(http.StatusInternalServerError, "internal", "The server could not complete that")
	}
	writeJSON(w, appErr.Status, map[string]string{"code": appErr.Code, "message": appErr.Message})
}
