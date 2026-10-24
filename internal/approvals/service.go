package approvals

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/audit"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type Decision struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	Version       int    `json:"version"`
	TargetVersion int    `json:"target_version"`
	ScopeID       string `json:"scope_id"`
}

func Create(ctx context.Context, tx pgx.Tx, actor platform.Actor, requestID, specID, reviewerID string, targetVersion int) (Decision, error) {
	var raw []byte
	err := tx.QueryRow(ctx, `
		SELECT app.create_scoped_approval($1,$2,'specification',$3,$4,$5)
	`, actor.OrgID, requestID, specID, targetVersion, reviewerID).Scan(&raw)
	if err != nil {
		return Decision{}, err
	}
	var decision Decision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return Decision{}, err
	}
	return decision, audit.Write(ctx, tx, actor, "approval.requested", "approval", decision.ID, decision)
}

func Decide(ctx context.Context, tx pgx.Tx, actor platform.Actor, approvalID, decisionName, comment, idempotencyKey string, version int, body []byte) (Decision, int, error) {
	scope := "approval:" + approvalID
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	if idempotencyKey != "" {
		var storedHash string
		var status int
		var response []byte
		err := tx.QueryRow(ctx, `
			SELECT body_hash, status, response FROM app.command_idempotency
			WHERE user_id = $1 AND scope = $2 AND idem_key = $3
		`, actor.ID, scope, idempotencyKey).Scan(&storedHash, &status, &response)
		if err == nil {
			if storedHash != hash {
				return Decision{}, http.StatusConflict, platform.Fail(http.StatusConflict, "idempotency_conflict", "This idempotency key was used with a different request")
			}
			var decision Decision
			if err := json.Unmarshal(response, &decision); err != nil {
				return Decision{}, 0, err
			}
			return decision, status, nil
		}
		if !platform.Missing(err) {
			return Decision{}, 0, err
		}
	}
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT app.decide_scoped_approval($1,$2,$3,$4)`, approvalID, decisionName, version, comment).Scan(&raw)
	if err != nil {
		return Decision{}, 0, err
	}
	var decision Decision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return Decision{}, 0, err
	}
	if idempotencyKey != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO app.command_idempotency (user_id, scope, idem_key, body_hash, status, response)
			VALUES ($1,$2,$3,$4,200,$5::jsonb)
		`, actor.ID, scope, idempotencyKey, hash, string(raw)); err != nil {
			return Decision{}, 0, err
		}
	}
	return decision, http.StatusOK, audit.Write(ctx, tx, actor, "approval."+decisionName, "approval", decision.ID, decision)
}
