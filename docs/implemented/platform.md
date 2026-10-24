# Implemented platform behavior

This note describes the FastAPI service that the Go API replaced. It is a record of that code, not a second implementation. Dashboard proposal handlers are omitted here.

## HTTP

`backend/src/app/main.py` mounts the API router. Liveness is `GET /api/health` and returns `{"status":"ok"}`. Readiness is `GET /api/ready` and checks the database. Business commands live under `/api/v1` in `backend/src/app/api/routes/v1.py`. Each response gets an `X-Request-Id`. Domain failures use `AppError` with an HTTP status, a stable `code`, and a `message`.

The Next.js app proxies `/api/:path*` to this process. The dashboard status badge calls `apiGet("/health")`, which fetches `/api/health`.

## Database session

`user_connection` opens a transaction, runs `SET LOCAL ROLE app_runtime`, then `set_config('app.user_id', ..., true)` and `set_config('app.user_email', ..., true)`. Those settings are transaction-local. The connection closes after commit or rollback.

`app_runtime` is created `NOLOGIN NOBYPASSRLS` in `supabase/migrations/20261024120000_app_baseline.sql`. It does not own the `app` tables. Table owners are whoever applied the migration. Row level security is enabled and forced. `app.current_user_id()` reads `app.user_id`.

`admin_connection` does not set the runtime role. It is used for local credential lookup and account creation. Business reads and writes from an authenticated request go through `user_connection`.

Postgres errors whose text contains `last_owner`, `project_cycle`, `parent_workspace_mismatch`, `workspace_mismatch`, `forbidden`, `not_found`, `invalid_target`, or `conflict` map to HTTP errors. Unique violations (`23505`) become conflicts.

## Authentication configuration

`Settings` defaults `environment` to `production` and `allow_local_auth` to false. `ALLOW_LOCAL_AUTH` is rejected unless `ENVIRONMENT` is `local` or `test`.

Local sessions exist only when that flag is on. `POST /api/v1/auth/local-session` creates or checks an `auth.users` row, an `app.profiles` row, and an `app.local_credentials` row. Passwords are scrypt hashes. The access token is HS256, with `sub`, `email`, the configured audience, and the configured issuer, and it expires in 12 hours.

`get_actor` requires a bearer token, checks the signature with `JWT_SECRET`, and loads `app.profiles`. A missing or non-active profile is unauthorized.

The shipped default for `jwt_secret` is the string `dev-only-change-me`. Startup does not reject that value, an empty secret, or a development issuer when `ENVIRONMENT` is `production`. That is a known defect. A production process with `ALLOW_LOCAL_AUTH=false` still accepts an HS256 token signed with that fallback for an active profile, because verification uses the same default secret the token was signed with.

## Verification limit

Tests that use plain Postgres plus this HS256 issuer cover schema, RLS, and FastAPI behavior. They do not prove Supabase Auth, session refresh, or Storage policies. Supabase CLI `start` in this environment needs Docker, which is not available. Convex cannot verify the local HS256 issuer. Its auth config accepts RS256 or ES256 JWKS only when `CONVEX_JWT_ISSUER` and a JWKS URL are set.
