ALTER TABLE analysis_runs
    ADD COLUMN IF NOT EXISTS target_section_id TEXT,
    ADD COLUMN IF NOT EXISTS target_section_title TEXT;
