package testdb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/khangdoan514/hacktx-2026/internal/platform"
)

const adminDSN = "postgresql://ubuntu@/postgres"
const testDSN = "postgresql://ubuntu@/hacktx_nmc3_test"

var once sync.Once
var setupErr error

func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	once.Do(func() {
		lock, err := os.OpenFile("/tmp/hacktx-nmc3-test.lock", os.O_CREATE|os.O_RDWR, 0o644)
		if err != nil {
			setupErr = err
			return
		}
		defer lock.Close()
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
			setupErr = err
			return
		}
		defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		conn, err := pgxpool.New(ctx, adminDSN)
		if err != nil {
			setupErr = err
			return
		}
		defer conn.Close()
		root, err := platform.ModuleRoot()
		if err != nil {
			setupErr = err
			return
		}
		sum := migrationHash(root)
		var ready string
		err = conn.QueryRow(ctx, `SELECT current_database()`).Scan(&ready)
		_ = err
		var marker string
		probe, probeErr := pgxpool.New(ctx, testDSN)
		if probeErr == nil {
			_ = probe.QueryRow(ctx, `SELECT value FROM public.nmc3_test_marker`).Scan(&marker)
			probe.Close()
		}
		if marker != sum {
			_, _ = conn.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'hacktx_nmc3_test' AND pid <> pg_backend_pid()")
			_, _ = conn.Exec(ctx, "DROP DATABASE IF EXISTS hacktx_nmc3_test")
			if _, setupErr = conn.Exec(ctx, "CREATE DATABASE hacktx_nmc3_test"); setupErr != nil {
				return
			}
			if setupErr = platform.ApplyMigrations(ctx, testDSN); setupErr != nil {
				return
			}
			mark, err := pgxpool.New(ctx, testDSN)
			if err != nil {
				setupErr = err
				return
			}
			_, setupErr = mark.Exec(ctx, `CREATE TABLE IF NOT EXISTS public.nmc3_test_marker (value text)`)
			if setupErr == nil {
				_, setupErr = mark.Exec(ctx, `INSERT INTO public.nmc3_test_marker VALUES ($1)`, sum)
			}
			mark.Close()
		}
	})
	if setupErr != nil {
		t.Fatal(setupErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(testDSN)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func migrationHash(root string) string {
	sum := sha256.New()
	matches, _ := filepath.Glob(filepath.Join(root, "supabase", "migrations", "*.sql"))
	for _, match := range matches {
		body, _ := os.ReadFile(match)
		sum.Write(body)
	}
	bootstrap, _ := os.ReadFile(filepath.Join(root, "supabase", "bootstrap_local.sql"))
	sum.Write(bootstrap)
	return hex.EncodeToString(sum.Sum(nil))
}

func DB(t *testing.T) *platform.DB {
	t.Helper()
	return &platform.DB{Pool: Pool(t)}
}
