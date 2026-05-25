package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/google"
	"technical-specification-review-agent/internal/repository"
)

const inboxProcessingComment = "Принял документ на проверку. Приступаю к анализу."

type inboxPendingAnalysis struct {
	documentExternalID string
	startedAt          time.Time
}

type GoogleInboxService struct {
	folderID         string
	pollInterval     time.Duration
	watcher          google.FolderWatcher
	documentRepo     repository.DocumentRepository
	analysisService  *AnalysisService
	commentService   *CommentService
	commentPublisher google.CommentPublisher

	mu      sync.Mutex
	pending map[string]inboxPendingAnalysis
}

func NewGoogleInboxService(
	folderID string,
	pollInterval time.Duration,
	watcher google.FolderWatcher,
	documentRepo repository.DocumentRepository,
	analysisService *AnalysisService,
	commentService *CommentService,
	commentPublisher google.CommentPublisher,
) *GoogleInboxService {
	return &GoogleInboxService{
		folderID:         strings.TrimSpace(folderID),
		pollInterval:     pollInterval,
		watcher:          watcher,
		documentRepo:     documentRepo,
		analysisService:  analysisService,
		commentService:   commentService,
		commentPublisher: commentPublisher,
		pending:          make(map[string]inboxPendingAnalysis),
	}
}

func (s *GoogleInboxService) Enabled() bool {
	return s != nil && s.folderID != "" && s.watcher != nil && s.analysisService != nil && s.commentService != nil && s.commentPublisher != nil
}

func (s *GoogleInboxService) Run(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	if err := s.tick(ctx); err != nil {
		log.Printf("google inbox tick: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.tick(ctx); err != nil {
				log.Printf("google inbox tick: %v", err)
			}
		}
	}
}

func (s *GoogleInboxService) tick(ctx context.Context) error {
	if err := s.enqueueNewDocuments(ctx); err != nil {
		return err
	}
	s.processPending(ctx)
	return nil
}

func (s *GoogleInboxService) enqueueNewDocuments(ctx context.Context) error {
	files, err := s.watcher.ListDocuments(ctx, s.folderID)
	if err != nil {
		return err
	}

	for _, file := range files {
		if s.isPendingDocument(file.ID) {
			continue
		}

		exists, err := s.documentRepo.HasBySourceAndExternalID(ctx, domain.DocumentSourceGoogleDocs, file.ID)
		if err != nil {
			return err
		}
		if exists {
			continue
		}

		if err := s.publishProcessingComment(ctx, file.ID); err != nil {
			log.Printf("google inbox: document_id=%q publish processing comment: %v", file.ID, err)
		}

		analysis, err := s.analysisService.StartAnalysis(ctx, StartAnalysisInput{
			Name:         file.Name,
			Source:       domain.DocumentSourceGoogleDocs,
			Mode:         domain.AnalysisModeFullReview,
			GoogleDocURL: file.DocumentURL,
		})
		if err != nil {
			return fmt.Errorf("start inbox analysis for %s: %w", file.ID, err)
		}

		s.addPending(analysis.ID, file.ID)
		log.Printf("google inbox: queued analysis_id=%q document_id=%q", analysis.ID, file.ID)
	}

	return nil
}

func (s *GoogleInboxService) processPending(ctx context.Context) {
	for analysisID, pending := range s.snapshotPending() {
		analysis, err := s.analysisService.GetAnalysis(ctx, analysisID)
		if err != nil {
			log.Printf("google inbox: analysis_id=%q get status: %v", analysisID, err)
			continue
		}

		switch analysis.Status {
		case domain.AnalysisStatusCompleted:
			if _, err := s.commentService.PublishComments(ctx, PublishCommentsInput{
				AnalysisID: analysisID,
			}); err != nil {
				log.Printf("google inbox: analysis_id=%q auto publish failed: %v", analysisID, err)
				continue
			}
			log.Printf("google inbox: auto published analysis_id=%q document_id=%q", analysisID, pending.documentExternalID)
			s.removePending(analysisID)
		case domain.AnalysisStatusFailed:
			log.Printf("google inbox: analysis_id=%q failed", analysisID)
			s.removePending(analysisID)
		}
	}
}

func (s *GoogleInboxService) publishProcessingComment(ctx context.Context, documentExternalID string) error {
	return s.commentPublisher.Publish(ctx, documentExternalID, []google.CommentDraft{{
		Type:    "summary",
		Content: inboxProcessingComment,
	}})
}

func (s *GoogleInboxService) isPendingDocument(documentExternalID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pending := range s.pending {
		if pending.documentExternalID == documentExternalID {
			return true
		}
	}
	return false
}

func (s *GoogleInboxService) addPending(analysisID, documentExternalID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[analysisID] = inboxPendingAnalysis{
		documentExternalID: documentExternalID,
		startedAt:          time.Now().UTC(),
	}
}

func (s *GoogleInboxService) removePending(analysisID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pending, analysisID)
}

func (s *GoogleInboxService) snapshotPending() map[string]inboxPendingAnalysis {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make(map[string]inboxPendingAnalysis, len(s.pending))
	for key, value := range s.pending {
		result[key] = value
	}
	return result
}
