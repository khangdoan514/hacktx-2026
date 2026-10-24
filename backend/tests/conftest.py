import os
import tempfile
from pathlib import Path

import psycopg
import pytest

ROOT = Path(__file__).resolve().parents[2]
DSN = "postgresql://ubuntu@/hacktx_test"

os.environ["DATABASE_URL"] = DSN
os.environ["JWT_SECRET"] = "test-secret-test-secret-test-secret"
os.environ["JWT_ISSUER"] = "http://127.0.0.1/test-auth"
os.environ["JWT_AUDIENCE"] = "authenticated"
os.environ["ALLOW_LOCAL_AUTH"] = "true"
os.environ["STORAGE_DIR"] = tempfile.mkdtemp(prefix="nmc3-storage-")
os.environ["CONVEX_URL"] = ""
os.environ["CONVEX_DEPLOY_KEY"] = ""


def _admin():
    return psycopg.connect("postgresql://ubuntu@/postgres", autocommit=True)


def _rebuild() -> None:
    with _admin() as conn:
        conn.execute(
            "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'hacktx_test' AND pid <> pg_backend_pid()"
        )
        conn.execute("DROP DATABASE IF EXISTS hacktx_test")
        conn.execute("CREATE DATABASE hacktx_test OWNER ubuntu")
    with psycopg.connect(DSN, autocommit=True) as conn:
        conn.execute((ROOT / "supabase" / "bootstrap_local.sql").read_text())
        conn.execute((ROOT / "supabase" / "migrations" / "20261024120000_app_baseline.sql").read_text())


_rebuild()


@pytest.fixture(autouse=True)
def clean_database():
    with psycopg.connect(DSN) as conn:
        conn.execute(
            """
            DO $$
            DECLARE row record;
            BEGIN
              FOR row IN SELECT tablename FROM pg_tables WHERE schemaname = 'app'
              LOOP
                EXECUTE 'TRUNCATE TABLE app.' || quote_ident(row.tablename) || ' RESTART IDENTITY CASCADE';
              END LOOP;
            END $$;
            """
        )
        conn.execute("DELETE FROM auth.users")
        conn.commit()
    yield
