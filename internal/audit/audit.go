package audit

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

func Write(ctx context.Context, tx pgx.Tx, actor platform.Actor, action, resourceType, resourceID string, data any) error {
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO app.internal_audit (organization_id, actor_id, action, resource_type, resource_id, data)
		VALUES ($1,$2,$3,$4,NULLIF($5,'')::uuid,$6::jsonb)
	`, actor.OrgID, actor.ID, action, resourceType, resourceID, string(body))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO app.internal_outbox (topic, payload) VALUES ($1,$2::jsonb)`, action, string(body))
	return err
}
