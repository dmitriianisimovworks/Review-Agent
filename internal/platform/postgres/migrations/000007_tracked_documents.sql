CREATE TABLE IF NOT EXISTS tracked_documents (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    external_id TEXT NOT NULL,
    name TEXT NOT NULL,
    document_url TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (source, external_id)
);

CREATE INDEX IF NOT EXISTS idx_tracked_documents_source ON tracked_documents(source);
