package context

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Open returns a resolver whose grants and snapshots are stored in Postgres.
// Apply agents/db/migrations/202610240010_nmc3_context.sql first. Artifact
// metadata, memory metadata, and object bodies stay in memory. Close releases
// the pool. Driver errors are not returned: they can contain the database URL
// or row values.
func Open(ctx context.Context, databaseURL string, opts ...Option) (*Resolver, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, invalid("database URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, unavailable()
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, unavailable()
	}
	resolver := New(append([]Option{
		WithGrantStore(&pgGrantStore{pool: pool}),
		WithSnapshotStore(&pgSnapshotStore{pool: pool}),
	}, opts...)...)
	resolver.close = pool.Close
	return resolver, nil
}

func unavailable() error {
	return &Error{Code: CodeInternal, Message: "database unavailable"}
}

type pgGrantStore struct {
	pool *pgxpool.Pool
}

type pgSnapshotStore struct {
	pool *pgxpool.Pool
}

const grantColumns = `id, tenant_id, principal_id, agent_id, scope, resource_id, purposes, expires_at, revoked_at, revoked_by`

func (s *pgGrantStore) Insert(ctx context.Context, grant Grant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `insert into nmc3.context_grants (`+grantColumns+`) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		grant.ID, grant.TenantID, grant.PrincipalID, grant.AgentID, string(grant.Scope), grant.ResourceID,
		purposeStrings(grant.Purposes), nullTime(grant.ExpiresAt), nullTime(grant.RevokedAt), grant.RevokedBy)
	if isUnique(err) {
		return invalid("grant already exists")
	}
	return hideStoreErr(err)
}

func (s *pgGrantStore) Put(ctx context.Context, grant Grant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `update nmc3.context_grants set tenant_id=$2, principal_id=$3, agent_id=$4, scope=$5, resource_id=$6, purposes=$7, expires_at=$8, revoked_at=$9, revoked_by=$10 where id=$1`,
		grant.ID, grant.TenantID, grant.PrincipalID, grant.AgentID, string(grant.Scope), grant.ResourceID,
		purposeStrings(grant.Purposes), nullTime(grant.ExpiresAt), nullTime(grant.RevokedAt), grant.RevokedBy)
	if err != nil {
		return hideStoreErr(err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("grant not found")
	}
	return nil
}

func (s *pgGrantStore) Get(ctx context.Context, id string) (Grant, bool, error) {
	if err := ctx.Err(); err != nil {
		return Grant{}, false, err
	}
	grant, err := scanGrant(s.pool.QueryRow(ctx, `select `+grantColumns+` from nmc3.context_grants where id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Grant{}, false, nil
	}
	if err != nil {
		return Grant{}, false, hideStoreErr(err)
	}
	return grant, true, nil
}

func (s *pgGrantStore) List(ctx context.Context, tenantID string) ([]Grant, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `select `+grantColumns+` from nmc3.context_grants where tenant_id=$1 order by id`, tenantID)
	if err != nil {
		return nil, hideStoreErr(err)
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			return nil, hideStoreErr(err)
		}
		out = append(out, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, hideStoreErr(err)
	}
	return out, nil
}

func (s *pgGrantStore) Decide(ctx context.Context, tenantID, principalID, agentID string, scope Scope, resource string, purpose Purpose, now time.Time) (Grant, Code, error) {
	if err := ctx.Err(); err != nil {
		return Grant{}, "", err
	}
	rows, err := s.pool.Query(ctx, `select `+grantColumns+` from nmc3.context_grants where tenant_id=$1 and principal_id=$2 and agent_id=$3 and scope=$4 and resource_id=$5 order by id`,
		tenantID, principalID, agentID, string(scope), resource)
	if err != nil {
		return Grant{}, "", hideStoreErr(err)
	}
	defer rows.Close()
	var matched []Grant
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			return Grant{}, "", hideStoreErr(err)
		}
		matched = append(matched, grant)
	}
	if err := rows.Err(); err != nil {
		return Grant{}, "", hideStoreErr(err)
	}
	grant, code := decideGrants(matched, purpose, now)
	return grant, code, nil
}

func (s *pgSnapshotStore) Put(ctx context.Context, snap Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return &Error{Code: CodeInternal, Message: codeMessage(CodeInternal)}
	}
	_, err = s.pool.Exec(ctx, `insert into nmc3.context_snapshots (id, version, tenant_id, run_id, invocation_id, agent_id, document, retain_until, redacted_at, created_at) values ($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10) on conflict (id) do update set version=excluded.version, tenant_id=excluded.tenant_id, run_id=excluded.run_id, invocation_id=excluded.invocation_id, agent_id=excluded.agent_id, document=excluded.document, retain_until=excluded.retain_until, redacted_at=excluded.redacted_at, created_at=excluded.created_at`,
		snap.ID, snap.Version, snap.TenantID, snap.RunID, snap.InvocationID, snap.AgentID, string(raw), snap.RetainUntil.UTC(), nullTime(snap.RedactedAt), snap.CreatedAt.UTC())
	return hideStoreErr(err)
}

func (s *pgSnapshotStore) Get(ctx context.Context, id string) (Snapshot, bool, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}
	var tenant string
	var raw []byte
	err := s.pool.QueryRow(ctx, `select tenant_id, document from nmc3.context_snapshots where id=$1`, id).Scan(&tenant, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, hideStoreErr(err)
	}
	snap, err := decodeSnapshot(tenant, raw)
	if err != nil {
		return Snapshot{}, false, err
	}
	return snap, true, nil
}

func (s *pgSnapshotStore) ListRun(ctx context.Context, tenantID, runID string) ([]Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `select tenant_id, document from nmc3.context_snapshots where tenant_id=$1 and run_id=$2 order by created_at, id`, tenantID, runID)
	if err != nil {
		return nil, hideStoreErr(err)
	}
	defer rows.Close()
	var out []Snapshot
	for rows.Next() {
		var tenant string
		var raw []byte
		if err := rows.Scan(&tenant, &raw); err != nil {
			return nil, hideStoreErr(err)
		}
		snap, err := decodeSnapshot(tenant, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, hideStoreErr(err)
	}
	return out, nil
}

func (s *pgSnapshotStore) RedactExpired(ctx context.Context, tenantID string, now time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, hideStoreErr(err)
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `select id, tenant_id, document from nmc3.context_snapshots where tenant_id=$1 and redacted_at is null and retain_until <= $2 for update`, tenantID, now.UTC())
	if err != nil {
		return 0, hideStoreErr(err)
	}
	type pending struct {
		id   string
		snap Snapshot
	}
	var items []pending
	for rows.Next() {
		var id, tenant string
		var raw []byte
		if err := rows.Scan(&id, &tenant, &raw); err != nil {
			rows.Close()
			return 0, hideStoreErr(err)
		}
		snap, err := decodeSnapshot(tenant, raw)
		if err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, pending{id: id, snap: snap})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, hideStoreErr(err)
	}
	rows.Close()
	for _, item := range items {
		redactSnapshot(&item.snap, now)
		raw, err := json.Marshal(item.snap)
		if err != nil {
			return 0, &Error{Code: CodeInternal, Message: codeMessage(CodeInternal)}
		}
		if _, err := tx.Exec(ctx, `update nmc3.context_snapshots set document=$2::jsonb, redacted_at=$3 where id=$1 and tenant_id=$4`, item.id, string(raw), item.snap.RedactedAt.UTC(), tenantID); err != nil {
			return 0, hideStoreErr(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, hideStoreErr(err)
	}
	return len(items), nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanGrant(row rowScanner) (Grant, error) {
	var grant Grant
	var scope string
	var purposes []string
	if err := row.Scan(&grant.ID, &grant.TenantID, &grant.PrincipalID, &grant.AgentID, &scope, &grant.ResourceID, &purposes, &grant.ExpiresAt, &grant.RevokedAt, &grant.RevokedBy); err != nil {
		return Grant{}, err
	}
	grant.Scope = Scope(scope)
	grant.Purposes = make([]Purpose, len(purposes))
	for i, purpose := range purposes {
		grant.Purposes[i] = Purpose(purpose)
	}
	grant.ExpiresAt = cloneTime(grant.ExpiresAt)
	grant.RevokedAt = cloneTime(grant.RevokedAt)
	return grant, nil
}

func decodeSnapshot(tenant string, raw []byte) (Snapshot, error) {
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Snapshot{}, &Error{Code: CodeInternal, Message: codeMessage(CodeInternal)}
	}
	if snap.TenantID != tenant {
		return Snapshot{}, &Error{Code: CodeInternal, Message: codeMessage(CodeInternal)}
	}
	if len(snap.MemoryRefs) == 0 {
		snap.MemoryRefs = nil
	}
	return snap, nil
}

func purposeStrings(in []Purpose) []string {
	out := make([]string, len(in))
	for i, purpose := range in {
		out[i] = string(purpose)
	}
	return out
}

func nullTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func hideStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &Error{Code: CodeInternal, Message: codeMessage(CodeInternal)}
}

var (
	_ GrantStore    = (*grantIndex)(nil)
	_ GrantStore    = (*pgGrantStore)(nil)
	_ SnapshotStore = (*snapshotIndex)(nil)
	_ SnapshotStore = (*pgSnapshotStore)(nil)
)
