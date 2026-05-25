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
const inboxProgressComment = "Анализ продолжается. Формирую итоговые замечания и summary."

const inboxProgressCommentDelay = 20 * time.Second

type inboxPendingAnalysis struct {
	documentExternalID  string
	startedAt           time.Time
	progressCommentSent bool
	serviceCommentIDs   []string
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

		serviceCommentIDs, err := s.publishServiceComment(ctx, file.ID, inboxProcessingComment)
		if err != nil {
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

		s.addPending(analysis.ID, file.ID, serviceCommentIDs)
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
			if err := s.deleteServiceComments(ctx, pending.documentExternalID, pending.serviceCommentIDs); err != nil {
				log.Printf("google inbox: analysis_id=%q cleanup service comments failed: %v", analysisID, err)
			}
			log.Printf("google inbox: auto published analysis_id=%q document_id=%q", analysisID, pending.documentExternalID)
			s.removePending(analysisID)
		case domain.AnalysisStatusFailed:
			log.Printf("google inbox: analysis_id=%q failed", analysisID)
			s.removePending(analysisID)
		default:
			if !pending.progressCommentSent && time.Since(pending.startedAt) >= inboxProgressCommentDelay {
				commentIDs, err := s.publishServiceComment(ctx, pending.documentExternalID, inboxProgressComment)
				if err != nil {
					log.Printf("google inbox: analysis_id=%q publish progress comment: %v", analysisID, err)
					continue
				}
				s.markProgressCommentSent(analysisID, commentIDs)
			}
		}
	}
}

func (s *GoogleInboxService) publishServiceComment(ctx context.Context, documentExternalID, content string) ([]string, error) {
	published, err := s.commentPublisher.Publish(ctx, documentExternalID, []google.CommentDraft{{
		Type:    "summary",
		Content: content,
	}})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(published))
	for _, comment := range published {
		if comment.ID != "" {
			ids = append(ids, comment.ID)
		}
	}
	return ids, nil
}

func (s *GoogleInboxService) deleteServiceComments(ctx context.Context, documentExternalID string, commentIDs []string) error {
	if len(commentIDs) == 0 {
		return nil
	}
	return s.commentPublisher.Delete(ctx, documentExternalID, commentIDs)
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

func (s *GoogleInboxService) addPending(analysisID, documentExternalID string, serviceCommentIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[analysisID] = inboxPendingAnalysis{
		documentExternalID:  documentExternalID,
		startedAt:           time.Now().UTC(),
		serviceCommentIDs:   append([]string(nil), serviceCommentIDs...),
		progressCommentSent: false,
	}
}

func (s *GoogleInboxService) markProgressCommentSent(analysisID string, commentIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.pending[analysisID]
	if !ok {
		return
	}
	pending.progressCommentSent = true
	pending.serviceCommentIDs = append(pending.serviceCommentIDs, commentIDs...)
	s.pending[analysisID] = pending
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
