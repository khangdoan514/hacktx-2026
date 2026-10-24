package requests

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/audit"
	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type Handover struct {
	ID          string          `json:"id"`
	RequestID   string          `json:"request_id"`
	State       string          `json:"state"`
	Version     int             `json:"version"`
	Snapshot    json.RawMessage `json:"snapshot"`
	RecipientID string          `json:"recipient_id"`
	Note        string          `json:"note"`
}

func CreateHandover(ctx context.Context, tx pgx.Tx, actor platform.Actor, requestID string) (Handover, error) {
	if err := organization.Require(actor, "ai_engineer", "operator", "administrator"); err != nil {
		return Handover{}, err
	}
	request, err := Get(ctx, tx, requestID)
	if err != nil {
		return Handover{}, err
	}
	if request.State != "in_delivery" && request.State != "accepted" {
		return Handover{}, platform.Fail(http.StatusConflict, "invalid_state", "Handover starts during delivery")
	}
	item := Handover{ID: platform.UUID(), RequestID: requestID, State: "draft", Version: 1, Snapshot: json.RawMessage(`{}`), RecipientID: request.RequesterID}
	_, err = tx.Exec(ctx, `
		INSERT INTO app.capability_handovers (id, organization_id, request_id, state, snapshot, recipient_id, created_by)
		VALUES ($1,$2,$3,'draft','{}'::jsonb,$4,$5)
	`, item.ID, actor.OrgID, requestID, request.RequesterID, actor.ID)
	return item, err
}

func SendHandover(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Handover, error) {
	if err := organization.Require(actor, "ai_engineer", "operator", "administrator"); err != nil {
		return Handover{}, err
	}
	var requestID string
	if err := tx.QueryRow(ctx, `SELECT request_id FROM app.capability_handovers WHERE id = $1`, id).Scan(&requestID); err != nil {
		return Handover{}, platform.Fail(http.StatusNotFound, "not_found", "Record not found")
	}
	var snapshot []byte
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(jsonb_build_object('specification_id', spec.id, 'revision', spec.material_revision, 'state', spec.state), '{}'::jsonb)
		FROM app.specifications AS spec WHERE spec.request_id = $1
	`, requestID).Scan(&snapshot); err != nil {
		snapshot = []byte(`{}`)
	}
	row := tx.QueryRow(ctx, `
		UPDATE app.capability_handovers
		SET state = 'sent', snapshot = $3::jsonb, version = version + 1, updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND state = 'draft' AND version = $4
		RETURNING id::text, request_id::text, state, version, snapshot, recipient_id::text, note
	`, id, actor.OrgID, string(snapshot), version)
	item, err := scanHandover(row)
	if platform.Missing(err) {
		return Handover{}, platform.Fail(http.StatusConflict, "conflict", "The handover cannot be sent")
	}
	if err != nil {
		return Handover{}, err
	}
	return item, audit.Write(ctx, tx, actor, "handover.sent", "handover", item.ID, item)
}

func transitionHandover(ctx context.Context, tx pgx.Tx, actor platform.Actor, id, from, to string, version int, recipientOnly bool) (Handover, error) {
	query := `
		UPDATE app.capability_handovers
		SET state = $4, version = version + 1, updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND state = $3 AND version = $5`
	args := []any{id, actor.OrgID, from, to, version}
	if recipientOnly {
		query += ` AND recipient_id = $6`
		args = append(args, actor.ID)
	}
	query += ` RETURNING id::text, request_id::text, state, version, snapshot, recipient_id::text, note`
	item, err := scanHandover(tx.QueryRow(ctx, query, args...))
	if platform.Missing(err) {
		return Handover{}, platform.Fail(http.StatusConflict, "conflict", "That handover transition is not available")
	}
	if err != nil {
		return Handover{}, err
	}
	return item, audit.Write(ctx, tx, actor, "handover."+to, "handover", item.ID, item)
}

func AcknowledgeHandover(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Handover, error) {
	return transitionHandover(ctx, tx, actor, id, "sent", "acknowledged", version, true)
}

func CompleteHandover(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Handover, error) {
	return transitionHandover(ctx, tx, actor, id, "acknowledged", "completed", version, true)
}

func ReturnHandover(ctx context.Context, tx pgx.Tx, actor platform.Actor, id, note string, version int) (Handover, error) {
	if _, err := tx.Exec(ctx, `UPDATE app.capability_handovers SET note = $2 WHERE id = $1`, id, note); err != nil {
		return Handover{}, err
	}
	return transitionHandover(ctx, tx, actor, id, "sent", "returned", version, true)
}

func CancelHandover(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, version int) (Handover, error) {
	if err := organization.Require(actor, "ai_engineer", "operator", "administrator"); err != nil {
		return Handover{}, err
	}
	item, err := scanHandover(tx.QueryRow(ctx, `
		UPDATE app.capability_handovers
		SET state = 'cancelled', version = version + 1, updated_at = now()
		WHERE id = $1 AND organization_id = $2 AND version = $3 AND state IN ('draft','sent')
		RETURNING id::text, request_id::text, state, version, snapshot, recipient_id::text, note
	`, id, actor.OrgID, version))
	if platform.Missing(err) {
		return Handover{}, platform.Fail(http.StatusConflict, "conflict", "The handover cannot be cancelled")
	}
	return item, err
}

func GetHandover(ctx context.Context, tx pgx.Tx, id string) (Handover, error) {
	item, err := scanHandover(tx.QueryRow(ctx, `
		SELECT id::text, request_id::text, state, version, snapshot, recipient_id::text, note
		FROM app.capability_handovers WHERE id = $1
	`, id))
	if platform.Missing(err) {
		return Handover{}, platform.Fail(http.StatusNotFound, "not_found", "Record not found")
	}
	return item, err
}

func scanHandover(row pgx.Row) (Handover, error) {
	var item Handover
	err := row.Scan(&item.ID, &item.RequestID, &item.State, &item.Version, &item.Snapshot, &item.RecipientID, &item.Note)
	return item, err
}
