package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func ApplyMigrations(ctx context.Context, databaseURL string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	files := []string{filepath.Join(root, "supabase", "bootstrap_local.sql")}
	matches, err := filepath.Glob(filepath.Join(root, "supabase", "migrations", "*.sql"))
	if err != nil {
		return err
	}
	sort.Strings(matches)
	files = append(files, matches...)
	if err := psql(ctx, databaseURL, `CREATE TABLE IF NOT EXISTS public.schema_migrations (filename text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	var existing int
	if err := queryOne(ctx, databaseURL, `SELECT count(*) FROM public.schema_migrations`, &existing); err != nil {
		return err
	}
	var organizations bool
	if err := queryOne(ctx, databaseURL, `SELECT to_regclass('app.organizations') IS NOT NULL`, &organizations); err != nil {
		return err
	}
	if existing == 0 && organizations {
		for _, file := range files {
			if err := psql(ctx, databaseURL, fmt.Sprintf(`INSERT INTO public.schema_migrations (filename) VALUES (%s) ON CONFLICT DO NOTHING`, quoteLiteral(filepath.Base(file)))); err != nil {
				return err
			}
		}
		return nil
	}
	for _, file := range files {
		var applied bool
		if err := queryOne(ctx, databaseURL, fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM public.schema_migrations WHERE filename = %s)`, quoteLiteral(filepath.Base(file))), &applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := psqlFile(ctx, databaseURL, file); err != nil {
			return err
		}
		if err := psql(ctx, databaseURL, fmt.Sprintf(`INSERT INTO public.schema_migrations (filename) VALUES (%s)`, quoteLiteral(filepath.Base(file)))); err != nil {
			return err
		}
	}
	return nil
}

func psqlFile(ctx context.Context, databaseURL, file string) error {
	cmd := exec.CommandContext(ctx, "psql", databaseURL, "-v", "ON_ERROR_STOP=1", "-f", file)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", file, err, out)
	}
	return nil
}

func psql(ctx context.Context, databaseURL, sql string) error {
	cmd := exec.CommandContext(ctx, "psql", databaseURL, "-v", "ON_ERROR_STOP=1", "-c", sql)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

func queryOne(ctx context.Context, databaseURL, sql string, dest any) error {
	cmd := exec.CommandContext(ctx, "psql", databaseURL, "-v", "ON_ERROR_STOP=1", "-t", "-A", "-c", sql)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	text := strings.TrimSpace(string(out))
	switch target := dest.(type) {
	case *int:
		_, err = fmt.Sscan(text, target)
	case *bool:
		*target = text == "t" || text == "true"
	default:
		err = fmt.Errorf("unsupported scan target")
	}
	return err
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func ModuleRoot() (string, error) { moduleRoot, err := findRoot(); return moduleRoot, err }

func moduleRoot() (string, error) { return findRoot() }

func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}
