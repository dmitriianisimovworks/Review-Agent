ALTER TABLE analysis_runs
ADD COLUMN IF NOT EXISTS suppressed_findings_count INTEGER NOT NULL DEFAULT 0;
