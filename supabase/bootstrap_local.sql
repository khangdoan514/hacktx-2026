-- Local PostgreSQL only. Supabase already provides auth.users.
-- Safe to run before migrations: the table is created only when missing.
CREATE SCHEMA IF NOT EXISTS auth;

CREATE TABLE IF NOT EXISTS auth.users (
    id uuid PRIMARY KEY,
    email text,
    created_at timestamptz NOT NULL DEFAULT now()
);
