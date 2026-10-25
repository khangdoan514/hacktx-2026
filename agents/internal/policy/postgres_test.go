package policy_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/khangdoan514/hacktx-2026/agents/internal/policy"
)

func TestPostgresDenialsAndCrossTenantReads(t *testing.T) {
	testDenialsAndCrossTenantReads(t, newPostgresLab(t))
}

func TestPostgresApprovedWriteExpiresBeforeDispatch(t *testing.T) {
	testApprovedWriteExpiresBeforeDispatch(t, newPostgresLab(t))
}

func TestPostgresApprovalExpirySelfDecisionAndRestart(t *testing.T) {
	testApprovalExpirySelfDecisionAndRestart(t, newPostgresLab(t))
}

func TestPostgresTimeoutDoesNotDispatchTwice(t *testing.T) {
	testTimeoutDoesNotDispatchTwice(t, newPostgresLab(t))
}

func TestPostgresConcurrentDecisionAndDispatch(t *testing.T) {
	testConcurrentDecisionAndDispatch(t, newPostgresLab(t))
}

func TestPostgresPinnedPolicyIgnoresLaterRevocation(t *testing.T) {
	testPinnedPolicyIgnoresLaterRevocation(t, newPostgresLab(t))
}

func TestPostgresProtectedClassificationAndRedaction(t *testing.T) {
	testProtectedClassificationAndRedaction(t, newPostgresLab(t))
}

var (
	policyMigrateOnce sync.Once
	policyMigrateErr  error
)

func newPostgresLab(t *testing.T) *lab {
	t.Helper()
	url := os.Getenv("NMC3_POLICY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set NMC3_POLICY_TEST_DATABASE_URL to run the policy Postgres store")
	}
	migratePolicyDB(t, url)
	ctx := context.Background()
	if err := truncatePolicy(ctx, url); err != nil {
		t.Fatal(err)
	}
	store, err := policy.OpenPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	clk := &lockedClock{at: time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)}
	secrets := &fakeSecrets{values: map[string]string{}}
	return &lab{
		eng:     policy.New(store, policy.WithClock(clk.Now), policy.WithSecrets(secrets)),
		store:   store,
		clk:     clk,
		secrets: secrets,
	}
}

func migratePolicyDB(t *testing.T, url string) {
	t.Helper()
	policyMigrateOnce.Do(func() {
		policyMigrateErr = applyPolicyMigrations(url)
	})
	if policyMigrateErr != nil {
		t.Fatal(policyMigrateErr)
	}
}

func applyPolicyMigrations(url string) error {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if _, err = conn.PgConn().Exec(ctx, `
		do $$ begin
			if not exists (select 1 from pg_roles where rolname = 'anon') then
				create role anon nologin;
			end if;
			if not exists (select 1 from pg_roles where rolname = 'authenticated') then
				create role authenticated nologin;
			end if;
			if not exists (select 1 from pg_roles where rolname = 'service_role') then
				create role service_role nologin;
			end if;
		end $$;
	`).ReadAll(); err != nil {
		return err
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return os.ErrNotExist
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "db", "migrations")
	for _, name := range []string{
		"202610240001_nmc3_pass1.sql",
		"202610240004_nmc3_approvals.sql",
		"202610240009_nmc3_policy_pg.sql",
	} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if _, err = conn.PgConn().Exec(ctx, string(body)).ReadAll(); err != nil {
			return err
		}
	}
	return nil
}

func truncatePolicy(ctx context.Context, url string) error {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, `
		truncate table
			nmc3.approval_decisions,
			nmc3.write_attempts,
			nmc3.approval_requests,
			nmc3.policy_audit,
			nmc3.agent_policies,
			nmc3.policy_definitions,
			nmc3.policy_runs,
			nmc3.policy_secret_refs,
			nmc3.policy_contexts,
			nmc3.policy_events`)
	return err
}
