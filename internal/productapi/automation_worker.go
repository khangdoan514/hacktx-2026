package productapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

// AutomationWorker claims durable schedule occurrences. Multiple API replicas
// can run this loop because claims use FOR UPDATE SKIP LOCKED and leases.
type AutomationWorker struct {
	Server *Server
	Client *http.Client
}

func (w *AutomationWorker) Loop(ctx context.Context) {
	if w.Client == nil {
		w.Client = &http.Client{Timeout: 30 * time.Second}
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := w.Tick(ctx); err != nil && ctx.Err() == nil {
			log.Printf("automation worker: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *AutomationWorker) Tick(ctx context.Context) error {
	if err := w.enqueueDue(ctx); err != nil {
		return err
	}
	for i := 0; i < 10; i++ {
		id, err := w.claim(ctx)
		if err != nil {
			return err
		}
		if id == "" {
			break
		}
		w.dispatch(ctx, id)
	}
	return w.reconcile(ctx)
}

func (w *AutomationWorker) enqueueDue(ctx context.Context) error {
	return w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+automationColumns+` FROM app.agent_automations
			WHERE status='active' AND next_run_at<=now()
			ORDER BY next_run_at,id FOR UPDATE SKIP LOCKED LIMIT 25`)
		if err != nil {
			return err
		}
		items := []agentAutomation{}
		for rows.Next() {
			item, err := scanAutomation(rows)
			if err != nil {
				rows.Close()
				return err
			}
			items = append(items, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, item := range items {
			if item.NextRunAt == nil {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO app.agent_automation_runs (automation_id,scheduled_for)
				VALUES ($1,$2) ON CONFLICT (automation_id,scheduled_for) DO NOTHING`, item.ID, item.NextRunAt); err != nil {
				return err
			}
			var next *time.Time
			if item.Schedule.Kind != "once" {
				next, err = nextAutomationTime(item.Schedule, now)
				if err != nil {
					return err
				}
			}
			status := "active"
			if next == nil {
				status = "completed"
			}
			if _, err := tx.Exec(ctx, `UPDATE app.agent_automations SET next_run_at=$2,status=$3,updated_at=now() WHERE id=$1`, item.ID, next, status); err != nil {
				return err
			}
		}
		return nil
	})
}

func (w *AutomationWorker) claim(ctx context.Context) (string, error) {
	var id string
	err := w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `WITH candidate AS (
			SELECT r.id FROM app.agent_automation_runs r
			JOIN app.agent_automations a ON a.id=r.automation_id
			WHERE a.status<>'paused' AND
			((r.state='queued' AND (r.lease_until IS NULL OR r.lease_until<=now()))
			 OR (r.state='dispatching' AND r.lease_until<=now()))
			ORDER BY r.scheduled_for,r.id FOR UPDATE OF r SKIP LOCKED LIMIT 1
		)
		UPDATE app.agent_automation_runs r SET state='dispatching',attempts=r.attempts+1,
			lease_until=now()+interval '2 minutes',updated_at=now()
		FROM candidate WHERE r.id=candidate.id RETURNING r.id::text`).Scan(&id)
	})
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return id, err
}

type automationDispatch struct {
	OwnerID         string
	Email           string
	ProjectID       *string
	GitHubBindingID *string
	AgentID         string
	AgentVersion    string
	Input           json.RawMessage
	Limits          json.RawMessage
	Attempts        int
}

func (w *AutomationWorker) loadDispatch(ctx context.Context, id string) (automationDispatch, error) {
	var job automationDispatch
	err := w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT a.owner_id::text,e.email,a.project_id::text,a.github_binding_id::text,a.agent_id,a.agent_version,a.input,a.limits,r.attempts
			FROM app.agent_automation_runs r
			JOIN app.agent_automations a ON a.id=r.automation_id
			JOIN app.employees e ON e.user_id=a.owner_id
			WHERE r.id=$1`, id).Scan(&job.OwnerID, &job.Email, &job.ProjectID, &job.GitHubBindingID,
			&job.AgentID, &job.AgentVersion, &job.Input, &job.Limits, &job.Attempts)
	})
	return job, err
}

func (w *AutomationWorker) dispatch(ctx context.Context, id string) {
	job, err := w.loadDispatch(ctx, id)
	if err != nil {
		w.finishDispatch(ctx, id, "", fmt.Errorf("load automation: %w", err), true, 3)
		return
	}
	var actor platform.Actor
	err = w.Server.DB.WithUser(ctx, job.OwnerID, job.Email, func(ctx context.Context, tx pgx.Tx) error {
		loaded, err := organization.Load(ctx, tx, job.OwnerID, job.Email)
		actor = loaded
		if err != nil {
			return err
		}
		return automationPermission(actor)
	})
	if err != nil {
		w.finishDispatch(ctx, id, "", err, false, job.Attempts)
		return
	}
	if job.GitHubBindingID != nil {
		if job.ProjectID == nil || job.AgentID != "github-repo-reviewer" || job.AgentVersion != "1.0.0" {
			w.finishDispatch(ctx, id, "", fmt.Errorf("GitHub automation has an invalid agent or project"), false, job.Attempts)
			return
		}
		var repository, branch string
		var repositoryID, installationID int64
		err = w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT b.full_name,b.branch_name,b.repository_id,b.installation_id
				FROM app.project_github_repositories b JOIN app.github_installations i ON i.installation_id=b.installation_id
				WHERE b.id=$1 AND b.project_id=$2 AND b.enabled AND b.branch_name IS NOT NULL AND i.status='active'`,
				*job.GitHubBindingID, *job.ProjectID).Scan(&repository, &branch, &repositoryID, &installationID)
		})
		if err != nil {
			w.finishDispatch(ctx, id, "", fmt.Errorf("GitHub repository is no longer available"), false, job.Attempts)
			return
		}
		err = w.Server.DB.WithUser(ctx, job.OwnerID, job.Email, func(ctx context.Context, tx pgx.Tx) error {
			_, err := projectAccess(ctx, tx, *job.ProjectID)
			return err
		})
		if err != nil {
			w.finishDispatch(ctx, id, "", err, false, job.Attempts)
			return
		}
		var question struct {
			Question string `json:"question"`
		}
		if json.Unmarshal(job.Input, &question) != nil || strings.TrimSpace(question.Question) == "" {
			w.finishDispatch(ctx, id, "", fmt.Errorf("GitHub report question is missing"), false, job.Attempts)
			return
		}
		scope, err := signGitHubScope(githubScopeClaims{BindingID: *job.GitHubBindingID, ProjectID: *job.ProjectID,
			ActorID: job.OwnerID, RepositoryID: repositoryID, InstallationID: installationID, AutomationRunID: id})
		if err != nil {
			w.finishDispatch(ctx, id, "", err, true, job.Attempts)
			return
		}
		job.Input, _ = json.Marshal(map[string]string{"question": question.Question, "repository": repository, "branch": branch, "scope": scope})
	} else if job.AgentID == "github-repo-reviewer" {
		w.finishDispatch(ctx, id, "", fmt.Errorf("GitHub reviewer has no repository binding"), false, job.Attempts)
		return
	}
	payload := map[string]any{"agent_id": job.AgentID, "agent_version": job.AgentVersion, "input": job.Input, "limits": job.Limits}
	if job.ProjectID != nil {
		payload["project_id"] = *job.ProjectID
	}
	body, err := json.Marshal(payload)
	if err == nil {
		body, err = w.Server.prepareAgentRunPayload(ctx, body, actor)
	}
	if err != nil {
		w.finishDispatch(ctx, id, "", err, false, job.Attempts)
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(w.Server.Config.AgentAPIURL, "/")+"/v1/agent-runs", bytes.NewReader(body))
	if err != nil {
		w.finishDispatch(ctx, id, "", err, false, job.Attempts)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	stampEmployee(request, actor)
	response, err := w.Client.Do(request)
	if err != nil {
		w.finishDispatch(ctx, id, "", err, true, job.Attempts)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		w.finishDispatch(ctx, id, "", fmt.Errorf("agent start returned %d: %s", response.StatusCode, strings.TrimSpace(string(message))), response.StatusCode >= 500, job.Attempts)
		return
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	if json.NewDecoder(response.Body).Decode(&started) != nil || started.RunID == "" {
		w.finishDispatch(ctx, id, "", fmt.Errorf("agent start returned no run ID"), true, job.Attempts)
		return
	}
	w.finishDispatch(ctx, id, started.RunID, nil, false, job.Attempts)
}

func (w *AutomationWorker) finishDispatch(ctx context.Context, id, runID string, failure error, retryable bool, attempts int) {
	if ctx.Err() != nil {
		return // the lease is reclaimed after a restart
	}
	if failure != nil {
		message := failure.Error()
		if len(message) > 1000 {
			message = message[:1000]
		}
		state := "failed"
		var retryAt *time.Time
		if retryable && attempts < 3 {
			state = "queued"
			at := time.Now().UTC().Add(time.Duration(attempts*15) * time.Second)
			retryAt = &at
		}
		err := w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE app.agent_automation_runs
				SET state=$2,error=$3,lease_until=$4,updated_at=now() WHERE id=$1 AND state='dispatching'`, id, state, message, retryAt)
			return err
		})
		if err != nil {
			log.Printf("automation %s failure save: %v", id, err)
		}
		return
	}
	err := w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE app.agent_automation_runs
			SET state='started',agent_run_id=$2,lease_until=NULL,error=NULL,updated_at=now()
			WHERE id=$1 AND state='dispatching'`, id, runID)
		return err
	})
	if err != nil {
		log.Printf("automation %s run ID save: %v", id, err)
	}
}

func (w *AutomationWorker) reconcile(ctx context.Context) error {
	var started []struct{ ID, RunID string }
	err := w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id::text,agent_run_id FROM app.agent_automation_runs
			WHERE state='started' AND agent_run_id IS NOT NULL ORDER BY updated_at LIMIT 20`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item struct{ ID, RunID string }
			if err := rows.Scan(&item.ID, &item.RunID); err != nil {
				return err
			}
			started = append(started, item)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	for _, item := range started {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(w.Server.Config.AgentAPIURL, "/")+"/v1/agent-runs/"+item.RunID, nil)
		if err != nil {
			continue
		}
		request.Header.Set(platform.InternalHeader, platform.InternalToken())
		response, err := w.Client.Do(request)
		if err != nil {
			continue
		}
		var body struct {
			Run struct {
				State string `json:"state"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"run"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil {
			continue
		}
		state := strings.ToLower(body.Run.State)
		if state != "succeeded" && state != "failed" && state != "cancelled" {
			continue
		}
		var message any
		if body.Run.Error != nil {
			message = body.Run.Error.Message
		}
		if err := w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE app.agent_automation_runs SET state=$2,error=$3,updated_at=now()
				WHERE id=$1 AND state='started'`, item.ID, state, message)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}
