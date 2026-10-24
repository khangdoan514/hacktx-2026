package requests

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/audit"
	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type Record struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Outcome      string `json:"outcome"`
	State        string `json:"state"`
	Version      int    `json:"version"`
	RequesterID  string `json:"requester_id"`
	SourceTeamID string `json:"source_team_id"`
	TargetTeamID string `json:"target_team_id"`
	CancelReason string `json:"cancel_reason,omitempty"`
}

type Clarification struct {
	ID       string  `json:"id"`
	Question string  `json:"question"`
	Answer   *string `json:"answer"`
}

func scan(row pgx.Row) (Record, error) {
	var record Record
	err := row.Scan(&record.ID, &record.Title, &record.Outcome, &record.State, &record.Version, &record.RequesterID, &record.SourceTeamID, &record.TargetTeamID, &record.CancelReason)
	return record, err
}

const requestColumns = `id::text, title, outcome, state, version, requester_id::text, source_team_id::text, target_team_id::text, cancel_reason`

func Create(ctx context.Context, tx pgx.Tx, actor platform.Actor, title, outcome string) (Record, error) {
	if err := organization.Require(actor, "requester", "administrator"); err != nil {
		return Record{}, err
	}
	id := platform.UUID()
	if _, err := tx.Exec(ctx, `
		INSERT INTO app.capability_requests (id, organization_id, source_team_id, target_team_id, requester_id, title, outcome, state)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'draft')
	`, id, actor.OrgID, platform.HardwareTeamID, platform.AITeamID, actor.ID, title, outcome); err != nil {
		return Record{}, err
	}
	record, err := Get(ctx, tx, id)
	if err != nil {
		return Record{}, err
	}
	return record, audit.Write(ctx, tx, actor, "request.drafted", "request", record.ID, record)
}

func SaveDraft(ctx context.Context, tx pgx.Tx, actor platform.Actor, id, title, outcome string, version int) (Record, error) {
	row := tx.QueryRow(ctx, `
		UPDATE app.capability_requests
		SET title = $3, outcome = $4, version = version + 1, updated_at = now()
		WHERE id = $1 AND requester_id = $2 AND state = 'draft' AND version = $5
		RETURNING `+requestColumns, id, actor.ID, title, outcome, version)
	record, err := scan(row)
	if platform.Missing(err) {
		return Record{}, platform.Fail(http.StatusConflict, "conflict", "The draft changed or is no longer editable")
	}
	return record, err
}

func move(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int, from, to, action string, roles ...string) (Record, error) {
	if err := organization.Require(actor, roles...); err != nil {
		return Record{}, err
	}
	row := tx.QueryRow(ctx, `
		UPDATE app.capability_requests
		SET state = $4, version = version + 1, updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND state = $3 AND version = $5
		RETURNING `+requestColumns, id, actor.OrgID, from, to, version)
	record, err := scan(row)
	if platform.Missing(err) {
		return Record{}, platform.Fail(http.StatusConflict, "conflict", "That transition is not available")
	}
	if err != nil {
		return Record{}, err
	}
	return record, audit.Write(ctx, tx, actor, action, "request", record.ID, map[string]any{"state": to, "version": record.Version})
}

func Submit(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Record, error) {
	return move(ctx, tx, actor, id, version, "draft", "submitted", "request.submitted", "requester", "administrator")
}

func Triage(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Record, error) {
	return move(ctx, tx, actor, id, version, "submitted", "triage", "request.triaged", "ai_intake", "administrator")
}

func Accept(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Record, error) {
	return move(ctx, tx, actor, id, version, "triage", "accepted", "request.accepted", "ai_intake", "administrator")
}

func Reject(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Record, error) {
	return move(ctx, tx, actor, id, version, "triage", "rejected", "request.rejected", "ai_intake", "administrator")
}

func StartDelivery(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Record, error) {
	return move(ctx, tx, actor, id, version, "accepted", "in_delivery", "request.delivery_started", "ai_engineer", "administrator")
}

func MarkReady(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Record, error) {
	var sent bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM app.capability_handovers WHERE request_id = $1 AND state IN ('sent','acknowledged','completed'))
	`, id).Scan(&sent); err != nil {
		return Record{}, err
	}
	if !sent {
		return Record{}, platform.Fail(http.StatusConflict, "invalid_state", "Send the handover before asking for acceptance")
	}
	return move(ctx, tx, actor, id, version, "in_delivery", "ready_for_acceptance", "request.ready", "ai_engineer", "operator", "administrator")
}

func Fulfill(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Record, error) {
	current, err := Get(ctx, tx, id)
	if err != nil {
		return Record{}, err
	}
	if current.RequesterID != actor.ID {
		return Record{}, platform.Fail(http.StatusForbidden, "forbidden", "The requester acknowledges the handover")
	}
	var acknowledged bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM app.capability_handovers WHERE request_id = $1 AND state IN ('acknowledged','completed'))
	`, id).Scan(&acknowledged); err != nil {
		return Record{}, err
	}
	if !acknowledged {
		return Record{}, platform.Fail(http.StatusConflict, "invalid_state", "Acknowledge the handover first")
	}
	return move(ctx, tx, actor, id, version, "ready_for_acceptance", "fulfilled", "request.fulfilled", "requester", "administrator")
}

func Cancel(ctx context.Context, tx pgx.Tx, actor platform.Actor, id, reason string, version int) (Record, error) {
	row := tx.QueryRow(ctx, `
		UPDATE app.capability_requests
		SET state = 'cancelled', cancel_reason = $3, version = version + 1, updated_at = now()
		WHERE id = $1 AND requester_id = $2 AND version = $4
		  AND state NOT IN ('fulfilled','cancelled')
		RETURNING `+requestColumns, id, actor.ID, reason, version)
	record, err := scan(row)
	if platform.Missing(err) {
		return Record{}, platform.Fail(http.StatusConflict, "conflict", "That request cannot be cancelled")
	}
	if err != nil {
		return Record{}, err
	}
	return record, audit.Write(ctx, tx, actor, "request.cancelled", "request", id, map[string]string{"reason": reason})
}

func Reopen(ctx context.Context, tx pgx.Tx, actor platform.Actor, id, reason string, version int) (Record, error) {
	row := tx.QueryRow(ctx, `
		UPDATE app.capability_requests
		SET state = 'draft', cancel_reason = $3, version = version + 1, updated_at = now()
		WHERE id = $1 AND requester_id = $2 AND version = $4 AND state IN ('cancelled','rejected')
		RETURNING `+requestColumns, id, actor.ID, reason, version)
	record, err := scan(row)
	if platform.Missing(err) {
		return Record{}, platform.Fail(http.StatusConflict, "conflict", "That request cannot be reopened")
	}
	return record, err
}

func Clarify(ctx context.Context, tx pgx.Tx, actor platform.Actor, requestID, question string, version int) (Record, Clarification, error) {
	if _, err := move(ctx, tx, actor, requestID, version, "triage", "needs_information", "request.clarification_asked", "ai_intake", "administrator"); err != nil {
		return Record{}, Clarification{}, err
	}
	item := Clarification{ID: platform.UUID(), Question: question}
	_, err := tx.Exec(ctx, `
		INSERT INTO app.capability_clarifications (id, request_id, author_id, question) VALUES ($1,$2,$3,$4)
	`, item.ID, requestID, actor.ID, question)
	if err != nil {
		return Record{}, Clarification{}, err
	}
	record, err := Get(ctx, tx, requestID)
	return record, item, err
}

func Answer(ctx context.Context, tx pgx.Tx, actor platform.Actor, requestID, clarificationID, answer string, version int) (Record, error) {
	current, err := Get(ctx, tx, requestID)
	if err != nil {
		return Record{}, err
	}
	if current.RequesterID != actor.ID {
		return Record{}, platform.Fail(http.StatusForbidden, "forbidden", "The requester answers the question")
	}
	tag, err := tx.Exec(ctx, `
		UPDATE app.capability_clarifications
		SET answer = $3, answered_at = now()
		WHERE id = $1 AND request_id = $2 AND answer IS NULL
	`, clarificationID, requestID, answer)
	if err != nil {
		return Record{}, err
	}
	if tag.RowsAffected() != 1 {
		return Record{}, platform.Fail(http.StatusConflict, "conflict", "That question is already answered")
	}
	return move(ctx, tx, actor, requestID, version, "needs_information", "submitted", "request.clarification_answered", "requester", "administrator")
}

func Get(ctx context.Context, tx pgx.Tx, id string) (Record, error) {
	record, err := scan(tx.QueryRow(ctx, `SELECT `+requestColumns+` FROM app.capability_requests WHERE id = $1`, id))
	if platform.Missing(err) {
		return Record{}, platform.Fail(http.StatusNotFound, "not_found", "Record not found")
	}
	return record, err
}

func List(ctx context.Context, tx pgx.Tx) ([]Record, error) {
	rows, err := tx.Query(ctx, `SELECT `+requestColumns+` FROM app.capability_requests ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Record{}
	for rows.Next() {
		record, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	return items, rows.Err()
}

func Clarifications(ctx context.Context, tx pgx.Tx, requestID string) ([]Clarification, error) {
	rows, err := tx.Query(ctx, `SELECT id::text, question, answer FROM app.capability_clarifications WHERE request_id = $1 ORDER BY created_at`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Clarification{}
	for rows.Next() {
		var item Clarification
		if err := rows.Scan(&item.ID, &item.Question, &item.Answer); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func AddTask(ctx context.Context, tx pgx.Tx, actor platform.Actor, requestID, title string) (string, error) {
	if err := organization.Require(actor, "ai_engineer", "operator", "administrator"); err != nil {
		return "", err
	}
	id := platform.UUID()
	_, err := tx.Exec(ctx, `
		INSERT INTO app.delivery_tasks (id, organization_id, request_id, title, state, created_by)
		VALUES ($1,$2,$3,$4,'todo',$5)
	`, id, actor.OrgID, requestID, title, actor.ID)
	return id, err
}

func CompleteTask(ctx context.Context, tx pgx.Tx, actor platform.Actor, taskID, evidence string, version int) error {
	tag, err := tx.Exec(ctx, `
		UPDATE app.delivery_tasks
		SET state = 'done', evidence = $3, version = version + 1, updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND state = 'todo' AND version = $4
	`, taskID, actor.OrgID, evidence, version)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return platform.Fail(http.StatusConflict, "conflict", "The task changed")
	}
	return audit.Write(ctx, tx, actor, "task.completed", "task", taskID, map[string]string{"evidence": evidence})
}
