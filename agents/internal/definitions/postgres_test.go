package definitions

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresLifecycleAndTriggers(t *testing.T) {
	url := os.Getenv("NMC3_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set NMC3_TEST_DATABASE_URL to run definition registry integration")
	}
	ctx := context.Background()
	applyDefinitionMigration(t, url)

	tenant := fmt.Sprintf("t%d", time.Now().UnixNano())
	reg, err := Open(ctx, url, WithTenant(tenant))
	if err != nil {
		t.Fatal(dbErr(err))
	}
	t.Cleanup(reg.Close)

	spec := specialist("alpha", "1.0.0", "Alpha "+tenant)
	published, err := reg.Publish(ctx, "ada", spec)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := reg.PinRun(ctx, "run-postgres", spec.ID, spec.Version)
	if err != nil {
		t.Fatal(err)
	}
	next := spec
	next.Version = "1.1.0"
	next.Instructions = "version B"
	if _, err := reg.Publish(ctx, "grace", next); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Retire(ctx, "ida", spec.ID, spec.Version); err != nil {
		t.Fatal(err)
	}
	stored, err := reg.Get(ctx, spec.ID, spec.Version)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ContentHash != published.ContentHash || stored.Spec.Instructions != spec.Instructions {
		t.Fatal("retired postgres version changed")
	}
	pinned, err := reg.Pinned(ctx, "run-postgres", spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pinned.ContentHash != pin.ContentHash || pinned.Version != "1.0.0" {
		t.Fatal("postgres pin moved")
	}
	if _, err := reg.ResolveForNewRun(ctx, spec.ID, "1.0.0"); registryErr(t, err).Code != CodeRetired {
		t.Fatalf("resolve retired: %v", err)
	}
	if _, err := reg.ResolveForNewRun(ctx, spec.ID, "1.1.0"); err != nil {
		t.Fatal(err)
	}
	draft := specialist("draft-"+tenant, "1.0.0", "Draft "+tenant)
	if _, err := reg.SaveDraft(ctx, "ada", draft); err != nil {
		t.Fatal(err)
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `update nmc3.agent_versions set spec = jsonb_set(spec, '{instructions}', '"mutated"') where tenant_id=$1 and agent_id=$2 and version='1.1.0'`, tenant, spec.ID)
	if err == nil {
		t.Fatal("published row mutation was accepted")
	}
	_ = tx.Rollback(ctx)

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `update nmc3.agent_versions set spec = jsonb_set(spec, '{instructions}', '"draft edit"') where tenant_id=$1 and agent_id=$2 and version=$3`, tenant, draft.ID, draft.Version)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx)

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(ctx, `update nmc3.run_definition_pins set pinned_at = pinned_at where tenant_id=$1 and run_id=$2 and agent_id=$3`, tenant, "run-postgres", spec.ID)
	if err == nil {
		t.Fatal("pin mutation was accepted")
	}
	_ = tx.Rollback(ctx)
}

func TestPostgresWebsiteFlowAndPin(t *testing.T) {
	reg := openPostgresRegistry(t)
	exerciseWebsiteAndPin(t, reg)
}

func applyDefinitionMigration(t *testing.T, url string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgconn.Connect(ctx, url)
	if err != nil {
		t.Fatal(dbErr(err))
	}
	defer conn.Close(ctx)
	results, err := conn.Exec(ctx, migrationSQL(t)).ReadAll()
	if err != nil {
		t.Fatal(dbErr(err))
	}
	for _, result := range results {
		if result.Err != nil {
			t.Fatal(dbErr(result.Err))
		}
	}
}

func openPostgresRegistry(t *testing.T) *Registry {
	t.Helper()
	url := os.Getenv("NMC3_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set NMC3_TEST_DATABASE_URL to run definition registry integration")
	}
	applyDefinitionMigration(t, url)
	tenant := fmt.Sprintf("t%d", time.Now().UnixNano())
	reg, err := Open(context.Background(), url, WithTenant(tenant))
	if err != nil {
		t.Fatal(dbErr(err))
	}
	t.Cleanup(reg.Close)
	return reg
}

func dbErr(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if strings.Contains(message, "://") {
		return "postgres operation failed"
	}
	return message
}
