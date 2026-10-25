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

// GitHubDeliveryWorker turns signed webhook deliveries into one durable agent
// run per matching enabled rule. Claims make multiple API replicas safe.
type GitHubDeliveryWorker struct {
	Server *Server
	Client *http.Client
}

func (w *GitHubDeliveryWorker) Loop(ctx context.Context) {
	if w.Client == nil {
		w.Client = &http.Client{Timeout: 30 * time.Second}
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := w.Tick(ctx); err != nil && ctx.Err() == nil {
			log.Printf("github delivery worker: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *GitHubDeliveryWorker) Tick(ctx context.Context) error {
	if w.Client == nil {
		w.Client = &http.Client{Timeout: 30 * time.Second}
	}
	if err := w.fanOut(ctx); err != nil {
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

func (w *GitHubDeliveryWorker) fanOut(ctx context.Context) error {
	return w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT delivery_id::text FROM app.github_webhook_deliveries
			WHERE state='queued' ORDER BY received_at FOR UPDATE SKIP LOCKED LIMIT 25`)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			_, err := tx.Exec(ctx, `INSERT INTO app.github_rule_runs (delivery_id,rule_id)
				SELECT d.delivery_id,r.id FROM app.github_webhook_deliveries d
				JOIN app.project_github_repositories b ON b.repository_id=d.repository_id AND b.installation_id=d.installation_id AND b.enabled
					AND b.branch_name IS NOT NULL AND (d.event_name <> 'pull_request' OR d.payload->'pull_request'->'base'->>'ref'=b.branch_name)
				JOIN app.github_installations i ON i.installation_id=b.installation_id AND i.status='active'
				JOIN app.github_agent_rules r ON r.binding_id=b.id AND r.enabled AND r.event_name=d.event_name AND r.action_name=d.action_name
				WHERE d.delivery_id=$1 ON CONFLICT (delivery_id,rule_id) DO NOTHING`, id)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `UPDATE app.github_webhook_deliveries d SET state=CASE
				WHEN EXISTS (SELECT 1 FROM app.github_rule_runs r WHERE r.delivery_id=d.delivery_id) THEN 'processed'
				ELSE 'ignored' END,processed_at=now() WHERE delivery_id=$1`, id)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (w *GitHubDeliveryWorker) claim(ctx context.Context) (string, error) {
	var id string
	err := w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `WITH candidate AS (
			SELECT id FROM app.github_rule_runs WHERE
			(state='queued' AND (lease_until IS NULL OR lease_until<=now())) OR
			(state='dispatching' AND lease_until<=now())
			ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1
		)
		UPDATE app.github_rule_runs r SET state='dispatching',attempts=attempts+1,
			lease_until=now()+interval '2 minutes',updated_at=now()
		FROM candidate WHERE r.id=candidate.id RETURNING r.id::text`).Scan(&id)
	})
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return id, err
}

type githubDispatch struct {
	DeliveryID     string
	BindingID      string
	ProjectID      string
	OwnerID        string
	Email          string
	RepositoryID   int64
	InstallationID int64
	FullName       string
	Branch         string
	AgentID        string
	AgentVersion   string
	EventName      string
	ActionName     string
	Payload        json.RawMessage
	Attempts       int
	Enabled        bool
}

func (w *GitHubDeliveryWorker) load(ctx context.Context, id string) (githubDispatch, error) {
	var job githubDispatch
	err := w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT d.delivery_id::text,b.id::text,b.project_id::text,r.created_by::text,e.email,
			b.repository_id,b.installation_id,b.full_name,b.branch_name,r.agent_id,r.agent_version,d.event_name,d.action_name,d.payload,
			x.attempts,(b.enabled AND r.enabled AND i.status='active')
			FROM app.github_rule_runs x
			JOIN app.github_webhook_deliveries d ON d.delivery_id=x.delivery_id
			JOIN app.github_agent_rules r ON r.id=x.rule_id
			JOIN app.project_github_repositories b ON b.id=r.binding_id
			JOIN app.github_installations i ON i.installation_id=b.installation_id
			JOIN app.employees e ON e.user_id=r.created_by
			WHERE x.id=$1`, id).Scan(&job.DeliveryID, &job.BindingID, &job.ProjectID, &job.OwnerID, &job.Email,
			&job.RepositoryID, &job.InstallationID, &job.FullName, &job.Branch, &job.AgentID, &job.AgentVersion,
			&job.EventName, &job.ActionName, &job.Payload, &job.Attempts, &job.Enabled)
	})
	return job, err
}

func (w *GitHubDeliveryWorker) dispatch(ctx context.Context, id string) {
	job, err := w.load(ctx, id)
	if err != nil {
		w.finish(ctx, id, "", fmt.Errorf("load rule: %w", err), false, 3)
		return
	}
	if !job.Enabled || job.AgentID != "github-repo-reviewer" || job.AgentVersion != "1.0.0" {
		w.finish(ctx, id, "", fmt.Errorf("GitHub rule is disabled or uses an unsupported agent"), false, job.Attempts)
		return
	}
	var actor platform.Actor
	err = w.Server.DB.WithUser(ctx, job.OwnerID, job.Email, func(ctx context.Context, tx pgx.Tx) error {
		loaded, err := organization.Load(ctx, tx, job.OwnerID, job.Email)
		actor = loaded
		if err != nil {
			return err
		}
		if err := automationPermission(actor); err != nil {
			return err
		}
		_, err = projectAccess(ctx, tx, job.ProjectID)
		return err
	})
	if err != nil {
		w.finish(ctx, id, "", err, false, job.Attempts)
		return
	}
	scope, err := signGitHubScope(githubScopeClaims{BindingID: job.BindingID, ProjectID: job.ProjectID,
		ActorID: job.OwnerID, RepositoryID: job.RepositoryID, InstallationID: job.InstallationID, DeliveryID: job.DeliveryID})
	if err != nil {
		w.finish(ctx, id, "", err, true, job.Attempts)
		return
	}
	question := githubEventQuestion(job)
	input := map[string]string{"question": question, "repository": job.FullName, "branch": job.Branch, "scope": scope}
	limits := automationLimits{MaxDelegations: 1, MaxConcurrentAgents: 1, MaxToolCalls: 10,
		MaxModelTurns: 12, MaxTokens: 12000, MaxCostUSD: 2, TimeoutSeconds: 120}
	requestBody, _ := json.Marshal(map[string]any{"agent_id": job.AgentID, "agent_version": job.AgentVersion,
		"project_id": job.ProjectID, "input": input, "limits": limits})
	requestBody, err = w.Server.prepareAgentRunPayload(ctx, requestBody, actor)
	if err != nil {
		w.finish(ctx, id, "", err, false, job.Attempts)
		return
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(w.Server.Config.AgentAPIURL, "/")+"/v1/agent-runs", bytes.NewReader(requestBody))
	if err != nil {
		w.finish(ctx, id, "", err, false, job.Attempts)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	stampEmployee(request, actor)
	response, err := w.Client.Do(request)
	if err != nil {
		w.finish(ctx, id, "", err, true, job.Attempts)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		w.finish(ctx, id, "", fmt.Errorf("agent start returned %d", response.StatusCode), response.StatusCode >= 500, job.Attempts)
		return
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&started) != nil || started.RunID == "" {
		w.finish(ctx, id, "", fmt.Errorf("agent start returned no run ID"), true, job.Attempts)
		return
	}
	w.finish(ctx, id, started.RunID, nil, false, job.Attempts)
}

func githubEventQuestion(job githubDispatch) string {
	var event struct {
		Issue struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"issue"`
		PullRequest struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"pull_request"`
	}
	_ = json.Unmarshal(job.Payload, &event)
	if job.EventName == "issues" {
		return fmt.Sprintf("Review new issue #%d in %s on branch %s. Summarize progress impact and next steps. Use the repository tools and cite source URLs.", event.Issue.Number, job.FullName, job.Branch)
	}
	return fmt.Sprintf("Review pull request #%d targeting %s in %s after %s. Summarize changes, risks, and next steps. Use the repository tools and cite source URLs.", event.PullRequest.Number, job.Branch, job.FullName, job.ActionName)
}

func (w *GitHubDeliveryWorker) finish(ctx context.Context, id, runID string, failure error, retryable bool, attempts int) {
	if ctx.Err() != nil {
		return
	}
	state := "started"
	var message any
	var retryAt *time.Time
	if failure != nil {
		state = "failed"
		message = failure.Error()
		if retryable && attempts < 3 {
			state = "queued"
			at := time.Now().UTC().Add(time.Duration(attempts*15) * time.Second)
			retryAt = &at
		}
	}
	if err := w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE app.github_rule_runs SET state=$2,agent_run_id=NULLIF($3,''),
			error=$4,lease_until=$5,updated_at=now() WHERE id=$1 AND state='dispatching'`, id, state, runID, message, retryAt)
		return err
	}); err != nil {
		log.Printf("github rule run %s save: %v", id, err)
	}
}

func (w *GitHubDeliveryWorker) reconcile(ctx context.Context) error {
	var started []struct{ ID, RunID string }
	err := w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id::text,agent_run_id FROM app.github_rule_runs
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
		if err := w.Server.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE app.github_rule_runs SET state=$2,updated_at=now() WHERE id=$1 AND state='started'`, item.ID, state)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}
