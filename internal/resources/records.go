package resources

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/internal/organization"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

type Record struct {
	ID              string   `json:"id"`
	RequestID       string   `json:"request_id"`
	Kind            string   `json:"kind"`
	RequestedAmount *float64 `json:"requested_amount"`
	ApprovedAmount  *float64 `json:"approved_amount"`
	UsageAmount     *float64 `json:"usage_amount"`
	Unit            string   `json:"unit"`
}

func Create(ctx context.Context, tx pgx.Tx, actor platform.Actor, requestID, kind, unit string, requested float64) (Record, error) {
	role := "hardware_owner"
	if kind == "budget" {
		role = "budget_owner"
	}
	if err := organization.Require(actor, role, "requester", "ai_engineer", "administrator"); err != nil {
		return Record{}, err
	}
	if kind != "hardware" && kind != "budget" {
		return Record{}, platform.Fail(http.StatusUnprocessableEntity, "validation", "Kind must be hardware or budget")
	}
	item := Record{ID: platform.UUID(), RequestID: requestID, Kind: kind, RequestedAmount: &requested, Unit: unit}
	_, err := tx.Exec(ctx, `
		INSERT INTO app.resource_records (id, organization_id, request_id, kind, requested_amount, unit)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, item.ID, actor.OrgID, requestID, kind, requested, unit)
	return item, err
}

func Approve(ctx context.Context, tx pgx.Tx, actor platform.Actor, id string, approved float64) (Record, error) {
	var kind string
	if err := tx.QueryRow(ctx, `SELECT kind FROM app.resource_records WHERE id = $1`, id).Scan(&kind); err != nil {
		return Record{}, platform.Fail(http.StatusNotFound, "not_found", "Record not found")
	}
	role := "hardware_owner"
	if kind == "budget" {
		role = "budget_owner"
	}
	if err := organization.Require(actor, role, "administrator"); err != nil {
		return Record{}, err
	}
	_, err := tx.Exec(ctx, `UPDATE app.resource_records SET approved_amount = $2, updated_at = now() WHERE id = $1`, id, approved)
	if err != nil {
		return Record{}, err
	}
	return Get(ctx, tx, id)
}

func Get(ctx context.Context, tx pgx.Tx, id string) (Record, error) {
	var item Record
	err := tx.QueryRow(ctx, `
		SELECT id::text, request_id::text, kind, requested_amount, approved_amount, usage_amount, unit
		FROM app.resource_records WHERE id = $1
	`, id).Scan(&item.ID, &item.RequestID, &item.Kind, &item.RequestedAmount, &item.ApprovedAmount, &item.UsageAmount, &item.Unit)
	if platform.Missing(err) {
		return Record{}, platform.Fail(http.StatusNotFound, "not_found", "Record not found")
	}
	return item, err
}

func List(ctx context.Context, tx pgx.Tx, requestID string) ([]Record, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, request_id::text, kind, requested_amount, approved_amount, usage_amount, unit
		FROM app.resource_records WHERE request_id = $1 ORDER BY created_at
	`, requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Record{}
	for rows.Next() {
		var item Record
		if err := rows.Scan(&item.ID, &item.RequestID, &item.Kind, &item.RequestedAmount, &item.ApprovedAmount, &item.UsageAmount, &item.Unit); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
