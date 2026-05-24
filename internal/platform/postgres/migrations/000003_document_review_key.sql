ALTER TABLE documents
ADD COLUMN IF NOT EXISTS review_key TEXT;

UPDATE documents
SET review_key = CASE
    WHEN COALESCE(NULLIF(external_id, ''), '') <> '' THEN source || ':' || external_id
    ELSE source || ':' || lower(regexp_replace(COALESCE(name, 'unnamed_document'), '\s+', '_', 'g'))
END
WHERE review_key IS NULL OR review_key = '';

ALTER TABLE documents
ALTER COLUMN review_key SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_documents_review_key ON documents(review_key);
