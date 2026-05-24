CREATE TABLE IF NOT EXISTS google_oauth_connections (
    id TEXT PRIMARY KEY,
    google_user_id TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL,
    access_token TEXT NOT NULL,
    refresh_token TEXT,
    expiry TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_google_oauth_connections_email ON google_oauth_connections(email);

