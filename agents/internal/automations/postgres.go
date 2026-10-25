package automations

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore persists automations and firings in the agent database
// (agents/db/migrations/202610250002_nmc3_automations.sql).
type PostgresStore struct{ Pool *pgxpool.Pool }

const columns = `id,tenant_id,created_by,created_by_email,name,schedule,timezone,enabled,request,next_run_at,last_fired_at,created_at,updated_at`

func scan(row pgx.Row) (Automation, error) {
	var a Automation
	var raw []byte
	err := row.Scan(&a.ID, &a.TenantID, &a.CreatedBy, &a.CreatedByEmail, &a.Name, &a.Schedule, &a.Timezone, &a.Enabled, &raw, &a.NextRunAt, &a.LastFiredAt, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Automation{}, ErrNotFound
	}
	if err != nil {
		return Automation{}, err
	}
	return a, json.Unmarshal(raw, &a.Request)
}

func (s PostgresStore) Create(ctx context.Context, a Automation) error {
	raw, err := json.Marshal(a.Request)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `insert into nmc3.automations (`+columns+`) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		a.ID, a.TenantID, a.CreatedBy, a.CreatedByEmail, a.Name, a.Schedule, a.Timezone, a.Enabled, raw, a.NextRunAt, a.LastFiredAt, a.CreatedAt, a.UpdatedAt)
	return err
}

func (s PostgresStore) Update(ctx context.Context, a Automation) error {
	raw, err := json.Marshal(a.Request)
	if err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, `update nmc3.automations set name=$3,schedule=$4,timezone=$5,enabled=$6,request=$7,next_run_at=$8,updated_at=$9 where id=$1 and tenant_id=$2`,
		a.ID, a.TenantID, a.Name, a.Schedule, a.Timezone, a.Enabled, raw, a.NextRunAt, a.UpdatedAt)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s PostgresStore) Delete(ctx context.Context, tenant, id string) error {
	tag, err := s.Pool.Exec(ctx, `delete from nmc3.automations where id=$1 and tenant_id=$2`, id, tenant)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s PostgresStore) Get(ctx context.Context, tenant, id string) (Automation, error) {
	return scan(s.Pool.QueryRow(ctx, `select `+columns+` from nmc3.automations where id=$1 and tenant_id=$2`, id, tenant))
}

func (s PostgresStore) List(ctx context.Context, tenant string) ([]Automation, error) {
	rows, err := s.Pool.Query(ctx, `select `+columns+` from nmc3.automations where tenant_id=$1 order by created_at desc limit 200`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Automation{}
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Claim locks due rows with skip locked so two agent processes on the same
// database split the work instead of both firing it.
func (s PostgresStore) Claim(ctx context.Context, now time.Time, next func(Automation) *time.Time) ([]Automation, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `select `+columns+` from nmc3.automations where enabled and next_run_at <= $1 order by next_run_at limit 50 for update skip locked`, now)
	if err != nil {
		return nil, err
	}
	var due []Automation
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		due = append(due, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, a := range due {
		if _, err := tx.Exec(ctx, `update nmc3.automations set next_run_at=$2,last_fired_at=$3 where id=$1`, a.ID, next(a), now); err != nil {
			return nil, err
		}
	}
	return due, tx.Commit(ctx)
}

func (s PostgresStore) AddFiring(ctx context.Context, f Firing) error {
	var runID *string
	if f.RunID != "" {
		runID = &f.RunID
	}
	_, err := s.Pool.Exec(ctx, `insert into nmc3.automation_firings (id,automation_id,tenant_id,trigger,scheduled_for,fired_at,run_id,error) values ($1,$2,$3,$4,$5,$6,$7,$8)`,
		f.ID, f.AutomationID, f.TenantID, f.Trigger, f.ScheduledFor, f.FiredAt, runID, f.Error)
	return err
}

func (s PostgresStore) Firings(ctx context.Context, tenant, automationID string, limit int) ([]Firing, error) {
	rows, err := s.Pool.Query(ctx, `select f.id,f.automation_id,f.trigger,f.scheduled_for,f.fired_at,coalesce(f.run_id,''),f.error,a.name
		from nmc3.automation_firings f join nmc3.automations a on a.id=f.automation_id
		where f.tenant_id=$1 and ($2='' or f.automation_id=$2) order by f.fired_at desc limit $3`, tenant, automationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Firing{}
	for rows.Next() {
		f := Firing{TenantID: tenant}
		if err := rows.Scan(&f.ID, &f.AutomationID, &f.Trigger, &f.ScheduledFor, &f.FiredAt, &f.RunID, &f.Error, &f.Name); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
