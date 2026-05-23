package memory

import (
	"context"
	"errors"
	"sync"

	"technical-specification-review-agent/internal/domain"
)

var errDocumentNotFound = errors.New("document not found")

type DocumentRepository struct {
	mu    sync.RWMutex
	items map[string]domain.Document
}

func NewDocumentRepository() *DocumentRepository {
	return &DocumentRepository{
		items: make(map[string]domain.Document),
	}
}

func (r *DocumentRepository) Save(_ context.Context, document domain.Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.items[document.ID] = document
	return nil
}

func (r *DocumentRepository) GetByID(_ context.Context, id string) (domain.Document, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	document, ok := r.items[id]
	if !ok {
		return domain.Document{}, errDocumentNotFound
	}

	return document, nil
}
