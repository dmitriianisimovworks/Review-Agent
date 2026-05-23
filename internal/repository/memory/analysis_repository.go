package memory

import (
	"context"
	"errors"
	"sync"

	"technical-specification-review-agent/internal/domain"
)

var errAnalysisNotFound = errors.New("analysis not found")

type AnalysisRepository struct {
	mu    sync.RWMutex
	items map[string]domain.Analysis
}

func NewAnalysisRepository() *AnalysisRepository {
	return &AnalysisRepository{
		items: make(map[string]domain.Analysis),
	}
}

func (r *AnalysisRepository) Save(_ context.Context, analysis domain.Analysis) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.items[analysis.ID] = analysis
	return nil
}

func (r *AnalysisRepository) GetByID(_ context.Context, id string) (domain.Analysis, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	analysis, ok := r.items[id]
	if !ok {
		return domain.Analysis{}, errAnalysisNotFound
	}

	return analysis, nil
}
