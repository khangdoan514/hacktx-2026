package definitions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgStore struct {
	pool   *pgxpool.Pool
	tenant string
}

type pgTx struct {
	tx     pgx.Tx
	tenant string
}

func (s *pgStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *pgStore) Apply(ctx context.Context, fn func(txStore) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(&pgTx{tx: tx, tenant: s.tenant}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const versionColumns = `tenant_id, agent_id, version, schema_version, spec, content_hash, lifecycle_state, created_by, created_at, updated_at, tested_by, tested_at, published_by, published_at, retired_by, retired_at`

func (s *pgTx) GetVersion(ctx context.Context, id, version string) (Version, error) {
	row := s.tx.QueryRow(ctx, `select `+versionColumns+` from nmc3.agent_versions where tenant_id=$1 and agent_id=$2 and version=$3`, s.tenant, id, version)
	return scanVersion(row.Scan)
}

func (s *pgTx) ListVersions(ctx context.Context) ([]Version, error) {
	rows, err := s.tx.Query(ctx, `select `+versionColumns+` from nmc3.agent_versions where tenant_id=$1 order by agent_id, version`, s.tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		v, err := scanVersion(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *pgTx) NameAvailable(ctx context.Context, name, selfID string) error {
	var owner string
	err := s.tx.QueryRow(ctx, `select id from nmc3.agent_definitions where tenant_id=$1 and name=$2`, s.tenant, name).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if owner != selfID {
		return nameConflict()
	}
	return nil
}

func (s *pgTx) EnsureAgent(ctx context.Context, v Version) error {
	_, err := s.tx.Exec(ctx, `insert into nmc3.agent_definitions (tenant_id, id, name, lifecycle_state, created_by, created_at, updated_at) values ($1,$2,$3,$4,$5,$6,$7) on conflict (tenant_id, id) do nothing`,
		s.tenant, v.Spec.ID, v.Spec.Name, v.State, v.CreatedBy, v.CreatedAt.UTC(), v.UpdatedAt.UTC())
	return mapPG(err)
}

func (s *pgTx) PutVersion(ctx context.Context, v Version) error {
	_, err := s.GetVersion(ctx, v.Spec.ID, v.Spec.Version)
	if errors.Is(err, errNotFound) {
		return s.insertVersion(ctx, v)
	}
	if err != nil {
		return err
	}
	specJSON, err := canonicalJSON(v.Spec)
	if err != nil {
		return err
	}
	tag, err := s.tx.Exec(ctx, `update nmc3.agent_versions set schema_version=$1, spec=$2::jsonb, content_hash=$3, lifecycle_state=$4, updated_at=$5, tested_by=$6, tested_at=$7, published_by=$8, published_at=$9, retired_by=$10, retired_at=$11 where tenant_id=$12 and agent_id=$13 and version=$14 and lifecycle_state in ('draft','testing')`,
		v.SchemaVersion, string(specJSON), v.ContentHash, v.State, v.UpdatedAt.UTC(),
		nullString(v.TestedBy), nullTime(v.TestedAt), nullString(v.PublishedBy), nullTime(v.PublishedAt), nullString(v.RetiredBy), nullTime(v.RetiredAt),
		s.tenant, v.Spec.ID, v.Spec.Version)
	if err != nil {
		return mapPG(err)
	}
	if tag.RowsAffected() != 1 {
		return &Error{Code: CodeImmutable, Message: "published version cannot change; save a new version"}
	}
	return nil
}

func (s *pgTx) insertVersion(ctx context.Context, v Version) error {
	specJSON, err := canonicalJSON(v.Spec)
	if err != nil {
		return err
	}
	_, err = s.tx.Exec(ctx, `insert into nmc3.agent_versions (tenant_id, agent_id, version, schema_version, spec, content_hash, lifecycle_state, created_by, created_at, updated_at, tested_by, tested_at, published_by, published_at, retired_by, retired_at) values ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		s.tenant, v.Spec.ID, v.Spec.Version, v.SchemaVersion, string(specJSON), v.ContentHash, v.State,
		v.CreatedBy, v.CreatedAt.UTC(), v.UpdatedAt.UTC(),
		nullString(v.TestedBy), nullTime(v.TestedAt), nullString(v.PublishedBy), nullTime(v.PublishedAt), nullString(v.RetiredBy), nullTime(v.RetiredAt))
	return mapPG(err)
}

func (s *pgTx) RetireVersion(ctx context.Context, id, version, actor string, at time.Time) (Version, error) {
	tag, err := s.tx.Exec(ctx, `update nmc3.agent_versions set lifecycle_state='retired', retired_by=$1, retired_at=$2, updated_at=$2 where tenant_id=$3 and agent_id=$4 and version=$5 and lifecycle_state='published'`,
		actor, at.UTC(), s.tenant, id, version)
	if err != nil {
		return Version{}, mapPG(err)
	}
	if tag.RowsAffected() != 1 {
		existing, getErr := s.GetVersion(ctx, id, version)
		if getErr != nil {
			return Version{}, getErr
		}
		if existing.State == StateRetired {
			return existing, nil
		}
		return Version{}, &Error{Code: CodeInvalidInput, Message: "only a published version can be retired"}
	}
	return s.GetVersion(ctx, id, version)
}

func (s *pgTx) RefreshAgent(ctx context.Context, id, name string, at time.Time) error {
	tag, err := s.tx.Exec(ctx, `update nmc3.agent_definitions d set name=$3, lifecycle_state=coalesce((select case when bool_or(lifecycle_state='published') then 'published' when bool_or(lifecycle_state='testing') then 'testing' when bool_or(lifecycle_state='draft') then 'draft' else 'retired' end from nmc3.agent_versions v where v.tenant_id=d.tenant_id and v.agent_id=d.id), d.lifecycle_state), updated_at=$4 where d.tenant_id=$1 and d.id=$2`,
		s.tenant, id, name, at.UTC())
	if err != nil {
		return mapPG(err)
	}
	if tag.RowsAffected() != 1 {
		return errNotFound
	}
	return nil
}

func (s *pgTx) GetPin(ctx context.Context, runID, agentID string) (Pin, error) {
	var pin Pin
	var spec []byte
	err := s.tx.QueryRow(ctx, `select run_id, tenant_id, agent_id, version, content_hash, spec, pinned_at from nmc3.run_definition_pins where tenant_id=$1 and run_id=$2 and agent_id=$3`, s.tenant, runID, agentID).Scan(&pin.RunID, &pin.TenantID, &pin.AgentID, &pin.Version, &pin.ContentHash, &spec, &pin.PinnedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Pin{}, errNotFound
	}
	if err != nil {
		return Pin{}, err
	}
	if err := json.Unmarshal(spec, &pin.Spec); err != nil {
		return Pin{}, err
	}
	pin.PinnedAt = pin.PinnedAt.UTC()
	if pin.Spec.AllowedTools == nil {
		pin.Spec.AllowedTools = []string{}
	}
	if pin.Spec.AllowedDelegates == nil {
		pin.Spec.AllowedDelegates = []string{}
	}
	return pin, nil
}

func (s *pgTx) InsertPin(ctx context.Context, pin Pin) error {
	specJSON, err := canonicalJSON(pin.Spec)
	if err != nil {
		return err
	}
	_, err = s.tx.Exec(ctx, `insert into nmc3.run_definition_pins (run_id, tenant_id, agent_id, version, content_hash, spec, pinned_at) values ($1,$2,$3,$4,$5,$6::jsonb,$7)`,
		pin.RunID, s.tenant, pin.AgentID, pin.Version, pin.ContentHash, string(specJSON), pin.PinnedAt.UTC())
	return mapPG(err)
}

func (s *pgTx) ListPins(ctx context.Context, runID string) ([]Pin, error) {
	rows, err := s.tx.Query(ctx, `select run_id, tenant_id, agent_id, version, content_hash, spec, pinned_at from nmc3.run_definition_pins where tenant_id=$1 and run_id=$2 order by agent_id`, s.tenant, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Pin{}
	for rows.Next() {
		var pin Pin
		var spec []byte
		if err := rows.Scan(&pin.RunID, &pin.TenantID, &pin.AgentID, &pin.Version, &pin.ContentHash, &spec, &pin.PinnedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(spec, &pin.Spec); err != nil {
			return nil, err
		}
		pin.PinnedAt = pin.PinnedAt.UTC()
		out = append(out, pin)
	}
	return out, rows.Err()
}

func (s *pgTx) InsertTool(ctx context.Context, tool Tool, hash string, at time.Time) error {
	var existing string
	err := s.tx.QueryRow(ctx, `select content_hash from nmc3.tool_versions where tenant_id=$1 and tool_id=$2 and version=$3`, s.tenant, tool.ID, tool.Version).Scan(&existing)
	if err == nil {
		if existing == hash {
			return nil
		}
		return &Error{Code: CodeImmutable, Message: "tool version cannot change; register a new version"}
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err := s.tx.Exec(ctx, `insert into nmc3.tool_definitions (tenant_id, id, created_at) values ($1,$2,$3) on conflict (tenant_id, id) do nothing`, s.tenant, tool.ID, at.UTC()); err != nil {
		return mapPG(err)
	}
	specJSON, err := canonicalJSON(tool)
	if err != nil {
		return err
	}
	_, err = s.tx.Exec(ctx, `insert into nmc3.tool_versions (tenant_id, tool_id, version, spec, content_hash, side_effect, timeout_seconds, created_at) values ($1,$2,$3,$4::jsonb,$5,$6,$7,$8)`,
		s.tenant, tool.ID, tool.Version, string(specJSON), hash, tool.SideEffect, tool.TimeoutSeconds, at.UTC())
	return mapPG(err)
}

func (s *pgTx) ListTools(ctx context.Context) ([]Tool, error) {
	rows, err := s.tx.Query(ctx, `select spec from nmc3.tool_versions where tenant_id=$1 order by tool_id, version`, s.tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tool{}
	for rows.Next() {
		var spec []byte
		if err := rows.Scan(&spec); err != nil {
			return nil, err
		}
		var tool Tool
		if err := json.Unmarshal(spec, &tool); err != nil {
			return nil, err
		}
		out = append(out, tool)
	}
	return out, rows.Err()
}

func scanVersion(scan func(dest ...any) error) (Version, error) {
	var v Version
	var agentID, version string
	var spec []byte
	var testedBy, publishedBy, retiredBy *string
	var testedAt, publishedAt, retiredAt *time.Time
	err := scan(&v.TenantID, &agentID, &version, &v.SchemaVersion, &spec, &v.ContentHash, &v.State, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt, &testedBy, &testedAt, &publishedBy, &publishedAt, &retiredBy, &retiredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, errNotFound
	}
	if err != nil {
		return Version{}, err
	}
	if err := json.Unmarshal(spec, &v.Spec); err != nil {
		return Version{}, err
	}
	if v.Spec.ID != agentID || v.Spec.Version != version {
		return Version{}, &Error{Code: CodeConflict, Message: "stored definition identity does not match its spec"}
	}
	v.CreatedAt = v.CreatedAt.UTC()
	v.UpdatedAt = v.UpdatedAt.UTC()
	if testedBy != nil {
		v.TestedBy = *testedBy
	}
	if publishedBy != nil {
		v.PublishedBy = *publishedBy
	}
	if retiredBy != nil {
		v.RetiredBy = *retiredBy
	}
	v.TestedAt = utcPtr(testedAt)
	v.PublishedAt = utcPtr(publishedAt)
	v.RetiredAt = utcPtr(retiredAt)
	if v.Spec.AllowedTools == nil {
		v.Spec.AllowedTools = []string{}
	}
	if v.Spec.AllowedDelegates == nil {
		v.Spec.AllowedDelegates = []string{}
	}
	return v, nil
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	copied := t.UTC()
	return &copied
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func mapPG(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			if strings.Contains(pgErr.ConstraintName, "name") {
				return nameConflict()
			}
			return &Error{Code: CodeConflict, Message: "definition already exists"}
		case "23503":
			return &Error{Code: CodeConflict, Message: "definition reference is missing"}
		case "55000":
			return &Error{Code: CodeImmutable, Message: pgErr.Message}
		case "22023":
			return &Error{Code: CodeInvalidInput, Message: pgErr.Message}
		default:
			return fmt.Errorf("database operation failed: %w", err)
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	return err
}
