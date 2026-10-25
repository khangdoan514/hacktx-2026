package policy

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres is the Store backed by the Pass 4 approval tables and migration
// 202610240009_nmc3_policy_pg.sql. Authorization stays on Engine.
type Postgres struct {
	pool *pgxpool.Pool
}

var (
	_ Store = (*Postgres)(nil)
	_ Store = (*Memory)(nil)
)

// OpenPostgres connects to a Postgres database that already has the Pass 4
// migrations applied. The URL is not retained in errors.
func OpenPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	if databaseURL == "" {
		return nil, errCode(CodeInvalidInput, "database url is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, errCode(CodeInternal, "policy store is not configured")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errCode(CodeInternal, "policy store is not configured")
	}
	return &Postgres{pool: pool}, nil
}

// Close releases the pool.
func (p *Postgres) Close() {
	if p != nil && p.pool != nil {
		p.pool.Close()
	}
}

func (p *Postgres) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.pool == nil {
		return errCode(CodeInternal, "policy store is not configured")
	}
	return nil
}

func (p *Postgres) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	if err := p.ready(ctx); err != nil {
		return err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (p *Postgres) InsertPolicy(ctx context.Context, policy Policy) error {
	policy = copyPolicy(policy)
	err := p.tx(ctx, func(tx pgx.Tx) error {
		existing, err := policyAt(ctx, tx, policy.TenantID, policy.ID, policy.Revision)
		if err == nil {
			if samePolicy(existing, policy) {
				return nil
			}
			return errCode(CodeConflict, "policy revision already exists")
		}
		if !Is(err, CodeNotFound) {
			return err
		}
		_, err = tx.Exec(ctx, `
			insert into nmc3.agent_policies (
				id, tenant_id, revision, allowed_models, allowed_tools, allowed_delegates,
				protected_tools, approver_roles, max_tokens, max_cost_usd, max_model_turns,
				approval_ttl_seconds, escalation_role, allow_service_approval, created_by, created_at
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
			policy.ID, policy.TenantID, policy.Revision, textArray(policy.AllowedModels), textArray(policy.AllowedTools),
			textArray(policy.AllowedDelegates), textArray(policy.ProtectedTools), textArray(policy.ApproverRoles),
			policy.MaxTokens, policy.MaxCostUSD, policy.MaxModelTurns, ttlSeconds(policy.ApprovalTTL),
			policy.EscalationRole, policy.AllowServiceApproval, policy.CreatedBy, utc(policy.CreatedAt))
		return err
	})
	if uniqueViolation(err) {
		existing, readErr := p.Policy(ctx, policy.TenantID, policy.ID, policy.Revision)
		if readErr == nil && samePolicy(existing, policy) {
			return nil
		}
		return errCode(CodeConflict, "policy revision already exists")
	}
	return mapStoreErr(err)
}

func (p *Postgres) Policy(ctx context.Context, tenantID, id string, revision int) (Policy, error) {
	if err := p.ready(ctx); err != nil {
		return Policy{}, err
	}
	policy, err := policyAt(ctx, p.pool, tenantID, id, revision)
	return policy, mapStoreErr(err)
}

func (p *Postgres) LatestPolicy(ctx context.Context, tenantID, id string) (Policy, error) {
	if err := p.ready(ctx); err != nil {
		return Policy{}, err
	}
	row := p.pool.QueryRow(ctx, `select `+policyCols+` from nmc3.agent_policies where tenant_id=$1 and id=$2 order by revision desc limit 1`, tenantID, id)
	policy, err := scanPolicy(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, errCode(CodeNotFound, "policy not found")
	}
	return policy, mapStoreErr(err)
}

func (p *Postgres) InsertDefinition(ctx context.Context, def Definition) error {
	def.AllowedTools = cloneStrings(def.AllowedTools)
	def.AllowedDelegates = cloneStrings(def.AllowedDelegates)
	err := p.tx(ctx, func(tx pgx.Tx) error {
		existing, err := definitionAt(ctx, tx, def.TenantID, def.AgentID, def.Version)
		if err == nil {
			if sameDefinition(existing, def) {
				return nil
			}
			return errCode(CodeConflict, "definition version already exists")
		}
		if !Is(err, CodeNotFound) {
			return err
		}
		_, err = tx.Exec(ctx, `
			insert into nmc3.policy_definitions (tenant_id, agent_id, version, allowed_tools, allowed_delegates)
			values ($1,$2,$3,$4,$5)`,
			def.TenantID, def.AgentID, def.Version, textArray(def.AllowedTools), textArray(def.AllowedDelegates))
		return err
	})
	if uniqueViolation(err) {
		existing, readErr := p.Definition(ctx, def.TenantID, def.AgentID, def.Version)
		if readErr == nil && sameDefinition(existing, def) {
			return nil
		}
		return errCode(CodeConflict, "definition version already exists")
	}
	return mapStoreErr(err)
}

func (p *Postgres) Definition(ctx context.Context, tenantID, agentID, version string) (Definition, error) {
	if err := p.ready(ctx); err != nil {
		return Definition{}, err
	}
	def, err := definitionAt(ctx, p.pool, tenantID, agentID, version)
	return def, mapStoreErr(err)
}

func (p *Postgres) DefinitionInOtherTenant(ctx context.Context, agentID, version, notTenant string) (bool, error) {
	if err := p.ready(ctx); err != nil {
		return false, err
	}
	var ok bool
	err := p.pool.QueryRow(ctx, `
		select exists(
			select 1 from nmc3.policy_definitions
			where agent_id=$1 and version=$2 and tenant_id<>$3
		)`, agentID, version, notTenant).Scan(&ok)
	return ok, mapStoreErr(err)
}

func (p *Postgres) InsertRun(ctx context.Context, run RunRecord) (RunRecord, bool, error) {
	stored, created, err := p.insertRun(ctx, run)
	if uniqueViolation(err) {
		return p.insertRun(ctx, run)
	}
	if err != nil {
		return RunRecord{}, false, mapStoreErr(err)
	}
	return stored, created, nil
}

func (p *Postgres) insertRun(ctx context.Context, run RunRecord) (RunRecord, bool, error) {
	var stored RunRecord
	var created bool
	err := p.tx(ctx, func(tx pgx.Tx) error {
		existing, err := runAt(ctx, tx, run.ID)
		if err == nil {
			if equivalentRun(existing, run) {
				stored = existing
				return nil
			}
			return errCode(CodeConflict, "run id already exists")
		}
		if !Is(err, CodeNotFound) {
			return err
		}
		if run.IdempotencyKey != "" {
			existing, err = runByKey(ctx, tx, run.TenantID, run.IdempotencyKey)
			if err == nil {
				if equivalentRun(existing, run) {
					stored = existing
					return nil
				}
				return errCode(CodeConflict, "run idempotency key was already used")
			}
			if !Is(err, CodeNotFound) {
				return err
			}
		}
		if err := insertRunRow(ctx, tx, run); err != nil {
			return err
		}
		stored = run
		created = true
		return nil
	})
	return stored, created, err
}

func (p *Postgres) Run(ctx context.Context, id string) (RunRecord, error) {
	if err := p.ready(ctx); err != nil {
		return RunRecord{}, err
	}
	run, err := runAt(ctx, p.pool, id)
	return run, mapStoreErr(err)
}

func (p *Postgres) InsertSecret(ctx context.Context, ref SecretRef) error {
	err := p.tx(ctx, func(tx pgx.Tx) error {
		existing, err := secretAt(ctx, tx, ref.TenantID, ref.Name)
		if err == nil {
			if existing.RunID == ref.RunID {
				return nil
			}
			return errCode(CodeConflict, "secret reference already exists")
		}
		if !Is(err, CodeNotFound) {
			return err
		}
		_, err = tx.Exec(ctx, `insert into nmc3.policy_secret_refs (tenant_id, name, run_id) values ($1,$2,$3)`, ref.TenantID, ref.Name, ref.RunID)
		return err
	})
	if uniqueViolation(err) {
		existing, readErr := p.Secret(ctx, ref.TenantID, ref.Name)
		if readErr == nil && existing.RunID == ref.RunID {
			return nil
		}
		return errCode(CodeConflict, "secret reference already exists")
	}
	return mapStoreErr(err)
}

func (p *Postgres) Secret(ctx context.Context, tenantID, name string) (SecretRef, error) {
	if err := p.ready(ctx); err != nil {
		return SecretRef{}, err
	}
	ref, err := secretAt(ctx, p.pool, tenantID, name)
	return ref, mapStoreErr(err)
}

func (p *Postgres) SecretInOtherTenant(ctx context.Context, name, notTenant string) (bool, error) {
	if err := p.ready(ctx); err != nil {
		return false, err
	}
	var ok bool
	err := p.pool.QueryRow(ctx, `
		select exists(
			select 1 from nmc3.policy_secret_refs where name=$1 and tenant_id<>$2
		)`, name, notTenant).Scan(&ok)
	return ok, mapStoreErr(err)
}

func (p *Postgres) InsertContext(ctx context.Context, rec ContextRecord) error {
	err := p.tx(ctx, func(tx pgx.Tx) error {
		existing, err := contextAt(ctx, tx, rec.ID)
		if err == nil {
			if existing.TenantID == rec.TenantID && existing.RunID == rec.RunID && existing.Summary == rec.Summary {
				return nil
			}
			return errCode(CodeConflict, "context id already exists")
		}
		if !Is(err, CodeNotFound) {
			return err
		}
		_, err = tx.Exec(ctx, `insert into nmc3.policy_contexts (id, tenant_id, run_id, summary) values ($1,$2,$3,$4)`, rec.ID, rec.TenantID, rec.RunID, rec.Summary)
		return err
	})
	if uniqueViolation(err) {
		existing, readErr := p.Context(ctx, rec.ID)
		if readErr == nil && existing.TenantID == rec.TenantID && existing.RunID == rec.RunID && existing.Summary == rec.Summary {
			return nil
		}
		return errCode(CodeConflict, "context id already exists")
	}
	return mapStoreErr(err)
}

func (p *Postgres) Context(ctx context.Context, id string) (ContextRecord, error) {
	if err := p.ready(ctx); err != nil {
		return ContextRecord{}, err
	}
	rec, err := contextAt(ctx, p.pool, id)
	return rec, mapStoreErr(err)
}

func (p *Postgres) InsertEvents(ctx context.Context, runID string, events []EventRecord) error {
	return mapStoreErr(p.tx(ctx, func(tx pgx.Tx) error {
		for _, event := range events {
			if event.Sequence > math.MaxInt64 {
				return errCode(CodeInvalidInput, "event sequence is too large")
			}
			_, err := tx.Exec(ctx, `
				insert into nmc3.policy_events (run_id, sequence, event_type, data)
				values ($1,$2,$3,$4)`, runID, int64(event.Sequence), event.Type, event.Data)
			if err != nil {
				return err
			}
		}
		return nil
	}))
}

func (p *Postgres) Events(ctx context.Context, runID string) ([]EventRecord, error) {
	if err := p.ready(ctx); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
		select sequence, event_type, data from nmc3.policy_events
		where run_id=$1 order by id`, runID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	defer rows.Close()
	out := []EventRecord{}
	for rows.Next() {
		var seq int64
		event := EventRecord{RunID: runID}
		if err := rows.Scan(&seq, &event.Type, &event.Data); err != nil {
			return nil, mapStoreErr(err)
		}
		event.Sequence = uint64(seq)
		out = append(out, event)
	}
	return out, mapStoreErr(rows.Err())
}

func (p *Postgres) AddAudit(ctx context.Context, event AuditEvent) error {
	return mapStoreErr(p.tx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			insert into nmc3.policy_audit (
				id, tenant_id, actor_id, action, resource_kind, resource_id, outcome, code, detail, occurred_at
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			event.ID, event.TenantID, event.ActorID, event.Action, event.ResourceKind, event.ResourceID,
			event.Outcome, event.Code, event.Detail, utc(event.At))
		return err
	}))
}

func (p *Postgres) Audit(ctx context.Context, tenantID, resourceID string) ([]AuditEvent, error) {
	if err := p.ready(ctx); err != nil {
		return nil, err
	}
	rows, err := p.pool.Query(ctx, `
		select id, tenant_id, actor_id, action, resource_kind, resource_id, outcome, code, detail, occurred_at
		from nmc3.policy_audit
		where tenant_id=$1 and ($2='' or resource_id=$2)
		order by occurred_at, id`, tenantID, resourceID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(&event.ID, &event.TenantID, &event.ActorID, &event.Action, &event.ResourceKind, &event.ResourceID, &event.Outcome, &event.Code, &event.Detail, &event.At); err != nil {
			return nil, mapStoreErr(err)
		}
		event.At = event.At.UTC()
		out = append(out, event)
	}
	return out, mapStoreErr(rows.Err())
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const policyCols = `id, tenant_id, revision, allowed_models, allowed_tools, allowed_delegates, protected_tools, approver_roles, max_tokens, max_cost_usd, max_model_turns, approval_ttl_seconds, escalation_role, allow_service_approval, created_by, created_at`

func policyAt(ctx context.Context, q querier, tenantID, id string, revision int) (Policy, error) {
	row := q.QueryRow(ctx, `select `+policyCols+` from nmc3.agent_policies where tenant_id=$1 and id=$2 and revision=$3`, tenantID, id, revision)
	policy, err := scanPolicy(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, errCode(CodeNotFound, "policy revision not found")
	}
	return policy, err
}

func scanPolicy(scan func(dest ...any) error) (Policy, error) {
	var policy Policy
	var ttl int
	err := scan(&policy.ID, &policy.TenantID, &policy.Revision, &policy.AllowedModels, &policy.AllowedTools, &policy.AllowedDelegates, &policy.ProtectedTools, &policy.ApproverRoles, &policy.MaxTokens, &policy.MaxCostUSD, &policy.MaxModelTurns, &ttl, &policy.EscalationRole, &policy.AllowServiceApproval, &policy.CreatedBy, &policy.CreatedAt)
	if err != nil {
		return Policy{}, err
	}
	policy.AllowedModels = textArray(policy.AllowedModels)
	policy.AllowedTools = textArray(policy.AllowedTools)
	policy.AllowedDelegates = textArray(policy.AllowedDelegates)
	policy.ProtectedTools = textArray(policy.ProtectedTools)
	policy.ApproverRoles = textArray(policy.ApproverRoles)
	policy.ApprovalTTL = time.Duration(ttl) * time.Second
	policy.CreatedAt = policy.CreatedAt.UTC()
	return policy, nil
}

func definitionAt(ctx context.Context, q querier, tenantID, agentID, version string) (Definition, error) {
	var def Definition
	err := q.QueryRow(ctx, `
		select tenant_id, agent_id, version, allowed_tools, allowed_delegates
		from nmc3.policy_definitions where tenant_id=$1 and agent_id=$2 and version=$3`, tenantID, agentID, version).
		Scan(&def.TenantID, &def.AgentID, &def.Version, &def.AllowedTools, &def.AllowedDelegates)
	if errors.Is(err, pgx.ErrNoRows) {
		return Definition{}, errCode(CodeNotFound, "definition not found")
	}
	if err != nil {
		return Definition{}, err
	}
	def.AllowedTools = textArray(def.AllowedTools)
	def.AllowedDelegates = textArray(def.AllowedDelegates)
	return def, nil
}

func runAt(ctx context.Context, q querier, id string) (RunRecord, error) {
	row := q.QueryRow(ctx, `select `+runCols+` from nmc3.policy_runs where id=$1`, id)
	run, err := scanRun(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunRecord{}, errCode(CodeNotFound, "run not found")
	}
	return run, err
}

func runByKey(ctx context.Context, q querier, tenantID, key string) (RunRecord, error) {
	row := q.QueryRow(ctx, `select `+runCols+` from nmc3.policy_runs where tenant_id=$1 and idempotency_key=$2`, tenantID, key)
	run, err := scanRun(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunRecord{}, errCode(CodeNotFound, "run not found")
	}
	return run, err
}

const runCols = `id, tenant_id, requester_id, agent_id, version, model_id, policy_id, policy_revision, idempotency_key, state, resume_state, hold, max_tokens, max_cost_usd, max_model_turns, created_at`

func scanRun(scan func(dest ...any) error) (RunRecord, error) {
	var run RunRecord
	err := scan(&run.ID, &run.TenantID, &run.RequesterID, &run.AgentID, &run.Version, &run.ModelID, &run.PolicyID, &run.PolicyRevision, &run.IdempotencyKey, &run.State, &run.ResumeState, &run.Hold, &run.Budget.MaxTokens, &run.Budget.MaxCostUSD, &run.Budget.MaxModelTurns, &run.CreatedAt)
	if err != nil {
		return RunRecord{}, err
	}
	run.CreatedAt = run.CreatedAt.UTC()
	return run, nil
}

func insertRunRow(ctx context.Context, tx pgx.Tx, run RunRecord) error {
	_, err := tx.Exec(ctx, `
		insert into nmc3.policy_runs (
			id, tenant_id, requester_id, agent_id, version, model_id, policy_id, policy_revision,
			idempotency_key, state, resume_state, hold, max_tokens, max_cost_usd, max_model_turns, created_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		run.ID, run.TenantID, run.RequesterID, run.AgentID, run.Version, run.ModelID, run.PolicyID, run.PolicyRevision,
		run.IdempotencyKey, run.State, run.ResumeState, run.Hold, run.Budget.MaxTokens, run.Budget.MaxCostUSD, run.Budget.MaxModelTurns, utc(run.CreatedAt))
	return err
}

func secretAt(ctx context.Context, q querier, tenantID, name string) (SecretRef, error) {
	var ref SecretRef
	err := q.QueryRow(ctx, `select tenant_id, name, run_id from nmc3.policy_secret_refs where tenant_id=$1 and name=$2`, tenantID, name).
		Scan(&ref.TenantID, &ref.Name, &ref.RunID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SecretRef{}, errCode(CodeNotFound, "secret reference not found")
	}
	return ref, err
}

func contextAt(ctx context.Context, q querier, id string) (ContextRecord, error) {
	var rec ContextRecord
	err := q.QueryRow(ctx, `select id, tenant_id, run_id, summary from nmc3.policy_contexts where id=$1`, id).
		Scan(&rec.ID, &rec.TenantID, &rec.RunID, &rec.Summary)
	if errors.Is(err, pgx.ErrNoRows) {
		return ContextRecord{}, errCode(CodeNotFound, "context not found")
	}
	return rec, err
}

func holdRun(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `
		update nmc3.policy_runs
		set resume_state = case when state <> $2 then state else resume_state end,
			state = $2,
			hold = $3
		where id = $1`, id, StateWaiting, string(ApprovalWaiting))
	return err
}

func releaseRun(ctx context.Context, tx pgx.Tx, id, hold string) error {
	_, err := tx.Exec(ctx, `
		update nmc3.policy_runs
		set state = case
			when state = $2 and resume_state <> '' then resume_state
			when state = $2 then 'RUNNING'
			else state
		end,
		hold = $3
		where id = $1`, id, StateWaiting, hold)
	return err
}

func textArray(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func ttlSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	sec := int(d / time.Second)
	if sec == 0 {
		return 1
	}
	return sec
}

func utc(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC().Truncate(time.Microsecond)
}

func uniqueViolation(err error) bool {
	var pgerr *pgconn.PgError
	return errors.As(err, &pgerr) && pgerr.Code == "23505"
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	var target *Error
	if errors.As(err, &target) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		switch pgerr.Code {
		case "23505":
			return errCode(CodeConflict, "row already exists")
		case "23503":
			return errCode(CodeNotFound, "referenced row is missing")
		case "23514":
			return errCode(CodeConflict, "row violates a policy constraint")
		default:
			return errCode(CodeInternal, "policy store request failed ("+pgerr.Code+")")
		}
	}
	return errCode(CodeInternal, "policy store request failed")
}
