package productapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
)

// githubWebhook is public because GitHub cannot send an employee bearer token.
// The signature is verified against the raw bytes before parsing or database work.
func (s *Server) githubWebhook(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if secret == "" {
		http.Error(w, "GitHub webhooks are not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Webhook body is too large", http.StatusRequestEntityTooLarge)
		return
	}
	if !validGitHubSignature([]byte(secret), body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "Invalid webhook signature", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("X-GitHub-Event") == "ping" {
		w.WriteHeader(http.StatusOK)
		return
	}
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	if !projectUUID.MatchString(deliveryID) {
		http.Error(w, "Invalid delivery ID", http.StatusBadRequest)
		return
	}
	var event struct {
		Action              string `json:"action"`
		RepositoriesRemoved []struct {
			ID int64 `json:"id"`
		} `json:"repositories_removed"`
		Repository struct {
			ID int64 `json:"id"`
		} `json:"repository"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if !json.Valid(body) || json.Unmarshal(body, &event) != nil || event.Installation.ID <= 0 {
		http.Error(w, "Invalid webhook payload", http.StatusBadRequest)
		return
	}
	eventName := r.Header.Get("X-GitHub-Event")
	if len(eventName) == 0 || len(eventName) > 100 || len(event.Action) > 100 {
		http.Error(w, "Invalid webhook event", http.StatusBadRequest)
		return
	}
	if eventName == "installation" || eventName == "installation_repositories" {
		if err := s.updateGitHubInstallation(r.Context(), eventName, event.Action, event.Installation.ID, event.RepositoriesRemoved); err != nil {
			http.Error(w, "Installation change could not be recorded", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if event.Repository.ID <= 0 {
		http.Error(w, "Invalid webhook repository", http.StatusBadRequest)
		return
	}
	// We persist only events the first release can route to an agent.
	if !((eventName == "issues" && event.Action == "opened") ||
		(eventName == "pull_request" && (event.Action == "opened" || event.Action == "synchronize"))) {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var queued, inserted bool
	err = s.DB.Admin(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app.project_github_repositories b
			JOIN app.github_installations i ON i.installation_id=b.installation_id
			WHERE b.repository_id=$1 AND b.installation_id=$2 AND b.enabled AND i.status='active')`,
			event.Repository.ID, event.Installation.ID).Scan(&queued); err != nil {
			return err
		}
		state := "ignored"
		if queued {
			state = "queued"
		}
		return tx.QueryRow(ctx, `INSERT INTO app.github_webhook_deliveries
			(delivery_id,installation_id,repository_id,event_name,action_name,payload,state)
			VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7)
			ON CONFLICT (delivery_id) DO NOTHING RETURNING true`,
			deliveryID, event.Installation.ID, event.Repository.ID, eventName, event.Action, string(body), state).Scan(&inserted)
	})
	if err == pgx.ErrNoRows {
		inserted = false
		err = nil
	}
	if err != nil {
		http.Error(w, "Webhook could not be recorded", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]bool{"queued": queued, "duplicate": !inserted})
}

func (s *Server) updateGitHubInstallation(ctx context.Context, eventName, action string, installationID int64, removed []struct {
	ID int64 `json:"id"`
}) error {
	return s.DB.Admin(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if eventName == "installation" {
			status := ""
			switch action {
			case "deleted":
				status = "deleted"
			case "suspend":
				status = "suspended"
			case "unsuspend":
				status = "active"
			}
			if status != "" {
				if _, err := tx.Exec(ctx, `UPDATE app.github_installations SET status=$2,updated_at=now() WHERE installation_id=$1`, installationID, status); err != nil {
					return err
				}
			}
		}
		if eventName == "installation_repositories" && action == "removed" {
			for _, repo := range removed {
				if repo.ID <= 0 {
					continue
				}
				if _, err := tx.Exec(ctx, `UPDATE app.project_github_repositories SET enabled=false,updated_at=now()
					WHERE installation_id=$1 AND repository_id=$2`, installationID, repo.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func validGitHubSignature(secret, body []byte, header string) bool {
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	signature, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil || len(signature) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return hmac.Equal(signature, mac.Sum(nil))
}
