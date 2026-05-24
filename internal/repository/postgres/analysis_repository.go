package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"technical-specification-review-agent/internal/domain"
)

type AnalysisRepository struct {
	pool *pgxpool.Pool
}

func NewAnalysisRepository(pool *pgxpool.Pool) *AnalysisRepository {
	return &AnalysisRepository{pool: pool}
}

func (r *AnalysisRepository) Save(ctx context.Context, analysis domain.Analysis) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO analysis_runs (
			id, document_id, mode, status, summary, llm_provider, llm_model, chunk_count, error_message, created_at, completed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
	`, analysis.ID, analysis.DocumentID, analysis.Mode, analysis.Status, analysis.Summary, analysis.Provider, analysis.Model, analysis.ChunkCount, nullString(analysis.ErrorMessage), analysis.CreatedAt, analysis.CompletedAt); err != nil {
		return fmt.Errorf("insert analysis run: %w", err)
	}

	for _, chunk := range analysis.Chunks {
		if _, err := tx.Exec(ctx, `
			INSERT INTO analysis_chunks (
				id, analysis_run_id, chunk_index, chunk_text, prompt_version, system_prompt, user_prompt, raw_llm_response, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		`, chunk.ID, analysis.ID, chunk.ChunkIndex, chunk.ChunkText, chunk.PromptVersion, chunk.SystemPrompt, chunk.UserPrompt, chunk.RawLLMResponse, chunk.CreatedAt); err != nil {
			return fmt.Errorf("insert analysis chunk: %w", err)
		}
	}

	for idx, finding := range analysis.Findings {
		var chunkID any
		if finding.ChunkIndex >= 0 && finding.ChunkIndex < len(analysis.Chunks) {
			chunkID = analysis.Chunks[finding.ChunkIndex].ID
		}

		metadata, err := json.Marshal(map[string]any{
			"source_chunk_length": len(finding.SourceChunk),
			"finding_order":       idx,
		})
		if err != nil {
			return fmt.Errorf("marshal finding metadata: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO findings (
				id, analysis_run_id, chunk_id, role, category, severity, problem, why_it_is_bad, how_to_fix, source_excerpt, metadata_jsonb, created_at
			) VALUES (gen_random_uuid()::text, $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		`, analysis.ID, chunkID, finding.Role, finding.Category, finding.Severity, finding.Problem, finding.WhyItIsBad, finding.HowToFix, finding.SourceChunk, metadata, time.Now().UTC()); err != nil {
			return fmt.Errorf("insert finding: %w", err)
		}
	}

	summaryPayload, err := json.Marshal(map[string]any{"summary": analysis.Summary})
	if err != nil {
		return fmt.Errorf("marshal summary artifact: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO analysis_artifacts (id, analysis_run_id, artifact_type, payload_jsonb, created_at)
		VALUES (gen_random_uuid()::text, $1, $2, $3::jsonb, $4)
	`, analysis.ID, "summary", string(summaryPayload), time.Now().UTC()); err != nil {
		return fmt.Errorf("insert summary artifact: %w", err)
	}

	if analysis.Memory.HasContext() {
		memoryPayload, err := json.Marshal(analysis.Memory)
		if err != nil {
			return fmt.Errorf("marshal memory artifact: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO analysis_artifacts (id, analysis_run_id, artifact_type, payload_jsonb, created_at)
			VALUES (gen_random_uuid()::text, $1, $2, $3::jsonb, $4)
		`, analysis.ID, "memory_snapshot", string(memoryPayload), time.Now().UTC()); err != nil {
			return fmt.Errorf("insert memory artifact: %w", err)
		}
	}

	if len(analysis.DocumentSections) > 0 {
		structurePayload, err := json.Marshal(map[string]any{
			"sections": analysis.DocumentSections,
		})
		if err != nil {
			return fmt.Errorf("marshal structure artifact: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO analysis_artifacts (id, analysis_run_id, artifact_type, payload_jsonb, created_at)
			VALUES (gen_random_uuid()::text, $1, $2, $3::jsonb, $4)
		`, analysis.ID, "document_structure", string(structurePayload), time.Now().UTC()); err != nil {
			return fmt.Errorf("insert structure artifact: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

func (r *AnalysisRepository) GetByID(ctx context.Context, id string) (domain.Analysis, error) {
	var analysis domain.Analysis
	err := r.pool.QueryRow(ctx, `
		SELECT id, document_id, mode, status, summary, llm_provider, llm_model, chunk_count, COALESCE(error_message, ''), created_at, completed_at
		FROM analysis_runs
		WHERE id = $1
	`, id).Scan(
		&analysis.ID,
		&analysis.DocumentID,
		&analysis.Mode,
		&analysis.Status,
		&analysis.Summary,
		&analysis.Provider,
		&analysis.Model,
		&analysis.ChunkCount,
		&analysis.ErrorMessage,
		&analysis.CreatedAt,
		&analysis.CompletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Analysis{}, errNotFound
		}
		return domain.Analysis{}, err
	}

	rows, err := r.pool.Query(ctx, `
		SELECT role, category, severity, problem, why_it_is_bad, how_to_fix, source_excerpt
		FROM findings
		WHERE analysis_run_id = $1
		ORDER BY created_at ASC
	`, id)
	if err != nil {
		return domain.Analysis{}, err
	}
	defer rows.Close()

	findings := make([]domain.Finding, 0)
	for rows.Next() {
		var finding domain.Finding
		if err := rows.Scan(
			&finding.Role,
			&finding.Category,
			&finding.Severity,
			&finding.Problem,
			&finding.WhyItIsBad,
			&finding.HowToFix,
			&finding.SourceChunk,
		); err != nil {
			return domain.Analysis{}, err
		}
		findings = append(findings, finding)
	}
	analysis.Findings = findings

	return analysis, nil
}

func (r *AnalysisRepository) ListByReviewKey(ctx context.Context, reviewKey string, limit int) ([]domain.Analysis, error) {
	if limit <= 0 {
		limit = 5
	}

	runRows, err := r.pool.Query(ctx, `
		SELECT ar.id, ar.document_id, ar.mode, ar.status, ar.summary, ar.llm_provider, ar.llm_model, ar.chunk_count,
		       COALESCE(ar.error_message, ''), ar.created_at, ar.completed_at
		FROM analysis_runs ar
		JOIN documents d ON d.id = ar.document_id
		WHERE d.review_key = $1
		ORDER BY ar.created_at DESC
		LIMIT $2
	`, reviewKey, limit)
	if err != nil {
		return nil, err
	}
	defer runRows.Close()

	analyses := make([]domain.Analysis, 0, limit)
	analysisByID := make(map[string]*domain.Analysis, limit)
	ids := make([]string, 0, limit)

	for runRows.Next() {
		var analysis domain.Analysis
		if err := runRows.Scan(
			&analysis.ID,
			&analysis.DocumentID,
			&analysis.Mode,
			&analysis.Status,
			&analysis.Summary,
			&analysis.Provider,
			&analysis.Model,
			&analysis.ChunkCount,
			&analysis.ErrorMessage,
			&analysis.CreatedAt,
			&analysis.CompletedAt,
		); err != nil {
			return nil, err
		}
		analysis.Findings = make([]domain.Finding, 0)
		analyses = append(analyses, analysis)
		analysisByID[analysis.ID] = &analyses[len(analyses)-1]
		ids = append(ids, analysis.ID)
	}

	if len(ids) == 0 {
		return nil, nil
	}

	findingRows, err := r.pool.Query(ctx, `
		SELECT analysis_run_id, role, category, severity, problem, why_it_is_bad, how_to_fix, source_excerpt
		FROM findings
		WHERE analysis_run_id = ANY($1)
		ORDER BY created_at ASC
	`, ids)
	if err != nil {
		return nil, err
	}
	defer findingRows.Close()

	for findingRows.Next() {
		var analysisID string
		var finding domain.Finding
		if err := findingRows.Scan(
			&analysisID,
			&finding.Role,
			&finding.Category,
			&finding.Severity,
			&finding.Problem,
			&finding.WhyItIsBad,
			&finding.HowToFix,
			&finding.SourceChunk,
		); err != nil {
			return nil, err
		}
		if analysis := analysisByID[analysisID]; analysis != nil {
			analysis.Findings = append(analysis.Findings, finding)
		}
	}

	sort.SliceStable(analyses, func(i, j int) bool {
		return analyses[i].CreatedAt.After(analyses[j].CreatedAt)
	})

	return analyses, nil
}
