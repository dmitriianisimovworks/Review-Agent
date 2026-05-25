package service

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"technical-specification-review-agent/internal/apperrors"
	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/google"
	"technical-specification-review-agent/internal/repository"
)

const inboxProcessingComment = "Принял документ на проверку. Приступаю к анализу."
const inboxProgressComment = "Анализ продолжается. Формирую итоговые замечания и summary."
const inboxFullCommandAcceptedComment = "Команда принята. Запускаю полный review всего документа."
const inboxIncrementalCommandAcceptedComment = "Команда принята. Запускаю incremental review с учетом предыдущего контекста."
const inboxAlreadyProcessingComment = "Документ уже находится в обработке. Дождитесь завершения текущего review."

const inboxProgressCommentDelay = 20 * time.Second

const (
	reviewAgentCommandPrefix             = "@review-agent"
	reviewAgentCommandFull               = "full"
	reviewAgentCommandIncremental        = "incremental"
	reviewAgentCommandIncrementalSection = "incremental section:"
)

type reviewAgentCommand struct {
	mode               domain.AnalysisMode
	targetSectionTitle string
	createdAt          time.Time
}

type inboxPendingAnalysis struct {
	documentExternalID  string
	startedAt           time.Time
	progressCommentSent bool
}

type GoogleInboxService struct {
	folderID         string
	pollInterval     time.Duration
	watcher          google.FolderWatcher
	documentRepo     repository.DocumentRepository
	analysisService  *AnalysisService
	commentService   *CommentService
	commentReader    google.CommentReader
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
	commentReader google.CommentReader,
	commentPublisher google.CommentPublisher,
) *GoogleInboxService {
	return &GoogleInboxService{
		folderID:         strings.TrimSpace(folderID),
		pollInterval:     pollInterval,
		watcher:          watcher,
		documentRepo:     documentRepo,
		analysisService:  analysisService,
		commentService:   commentService,
		commentReader:    commentReader,
		commentPublisher: commentPublisher,
		pending:          make(map[string]inboxPendingAnalysis),
	}
}

func (s *GoogleInboxService) Enabled() bool {
	return s != nil && s.folderID != "" && s.watcher != nil && s.analysisService != nil && s.commentService != nil && s.commentReader != nil && s.commentPublisher != nil
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
	files, err := s.watcher.ListDocuments(ctx, s.folderID)
	if err != nil {
		return err
	}
	if err := s.enqueueNewDocuments(ctx, files); err != nil {
		return err
	}
	if err := s.processCommandComments(ctx, files); err != nil {
		return err
	}
	s.processPending(ctx)
	return nil
}

func (s *GoogleInboxService) enqueueNewDocuments(ctx context.Context, files []google.DriveFile) error {
	for _, file := range files {
		if s.isPendingDocument(file.ID) {
			if _, err := s.publishServiceComment(ctx, file.ID, inboxAlreadyProcessingComment); err != nil {
				log.Printf("google inbox: document_id=%q publish already-processing comment: %v", file.ID, err)
			}
			continue
		}

		exists, err := s.documentRepo.HasBySourceAndExternalID(ctx, domain.DocumentSourceGoogleDocs, file.ID)
		if err != nil {
			return err
		}
		if exists {
			continue
		}

		if _, err := s.publishServiceComment(ctx, file.ID, inboxProcessingComment); err != nil {
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

func (s *GoogleInboxService) processCommandComments(ctx context.Context, files []google.DriveFile) error {
	for _, file := range files {
		if s.isPendingDocument(file.ID) {
			continue
		}

		exists, err := s.documentRepo.HasBySourceAndExternalID(ctx, domain.DocumentSourceGoogleDocs, file.ID)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}

		command, ok, err := s.loadLatestCommand(ctx, file.ID)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if !s.isCommandNewerThanLatestCompleted(ctx, file.ID, command.createdAt) {
			continue
		}

		if _, err := s.publishServiceComment(ctx, file.ID, commandAcceptedComment(command)); err != nil {
			log.Printf("google inbox: document_id=%q publish processing comment: %v", file.ID, err)
		}
		if err := s.validateCommandTargetSection(ctx, file, command); err != nil {
			if _, publishErr := s.publishServiceComment(ctx, file.ID, fmt.Sprintf("Команда принята, но раздел %s не найден в структуре документа.", strings.TrimSpace(command.targetSectionTitle))); publishErr != nil {
				log.Printf("google inbox: document_id=%q publish section-not-found comment: %v", file.ID, publishErr)
			}
			continue
		}

		analysis, err := s.analysisService.StartAnalysis(ctx, StartAnalysisInput{
			Name:               file.Name,
			Source:             domain.DocumentSourceGoogleDocs,
			Mode:               command.mode,
			GoogleDocURL:       file.DocumentURL,
			TargetSectionTitle: command.targetSectionTitle,
		})
		if err != nil {
			return fmt.Errorf("start comment-triggered analysis for %s: %w", file.ID, err)
		}

		s.addPending(analysis.ID, file.ID)
		log.Printf("google inbox: command queued analysis_id=%q document_id=%q mode=%q target_section=%q", analysis.ID, file.ID, command.mode, command.targetSectionTitle)
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
		default:
			if !pending.progressCommentSent && time.Since(pending.startedAt) >= inboxProgressCommentDelay {
				if _, err := s.publishServiceComment(ctx, pending.documentExternalID, inboxProgressComment); err != nil {
					log.Printf("google inbox: analysis_id=%q publish progress comment: %v", analysisID, err)
					continue
				}
				s.markProgressCommentSent(analysisID)
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

func (s *GoogleInboxService) loadLatestCommand(ctx context.Context, documentExternalID string) (reviewAgentCommand, bool, error) {
	comments, err := s.commentReader.List(ctx, documentExternalID)
	if err != nil {
		return reviewAgentCommand{}, false, err
	}

	sort.SliceStable(comments, func(i, j int) bool {
		return comments[i].CreatedAt > comments[j].CreatedAt
	})
	for _, comment := range comments {
		if comment.Resolved {
			continue
		}
		command, ok := parseReviewAgentCommand(comment.Content, comment.CreatedAt)
		if ok {
			return command, true, nil
		}
	}
	return reviewAgentCommand{}, false, nil
}

func (s *GoogleInboxService) isCommandNewerThanLatestCompleted(ctx context.Context, documentExternalID string, createdAt time.Time) bool {
	analyses, err := s.analysisService.analysisRepo.ListByReviewKey(ctx, reviewKeyForGoogleDoc(documentExternalID), 1)
	if err != nil || len(analyses) == 0 {
		return true
	}
	return createdAt.After(analyses[0].CreatedAt)
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
		documentExternalID:  documentExternalID,
		startedAt:           time.Now().UTC(),
		progressCommentSent: false,
	}
}

func (s *GoogleInboxService) markProgressCommentSent(analysisID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.pending[analysisID]
	if !ok {
		return
	}
	pending.progressCommentSent = true
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

func parseReviewAgentCommand(content string, createdAtUnixNano int64) (reviewAgentCommand, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return reviewAgentCommand{}, false
	}

	lowered := strings.ToLower(trimmed)
	if !strings.HasPrefix(lowered, reviewAgentCommandPrefix) {
		return reviewAgentCommand{}, false
	}

	commandText := strings.TrimSpace(strings.TrimPrefix(lowered, reviewAgentCommandPrefix))
	createdAt := time.Unix(0, createdAtUnixNano).UTC()
	switch {
	case commandText == reviewAgentCommandFull:
		return reviewAgentCommand{mode: domain.AnalysisModeFullReview, createdAt: createdAt}, true
	case commandText == reviewAgentCommandIncremental:
		return reviewAgentCommand{mode: domain.AnalysisModeIncrementalReview, createdAt: createdAt}, true
	case strings.HasPrefix(commandText, reviewAgentCommandIncrementalSection):
		section := strings.TrimSpace(strings.TrimPrefix(commandText, reviewAgentCommandIncrementalSection))
		if section == "" {
			return reviewAgentCommand{}, false
		}
		return reviewAgentCommand{
			mode:               domain.AnalysisModeIncrementalReview,
			targetSectionTitle: section,
			createdAt:          createdAt,
		}, true
	default:
		return reviewAgentCommand{}, false
	}
}

func reviewKeyForGoogleDoc(documentExternalID string) string {
	return string(domain.DocumentSourceGoogleDocs) + ":" + strings.TrimSpace(strings.ToLower(documentExternalID))
}

func commandAcceptedComment(command reviewAgentCommand) string {
	if command.mode == domain.AnalysisModeFullReview {
		return inboxFullCommandAcceptedComment
	}
	if strings.TrimSpace(command.targetSectionTitle) != "" {
		return fmt.Sprintf("Команда принята. Запускаю incremental review для раздела %s.", strings.TrimSpace(command.targetSectionTitle))
	}
	return inboxIncrementalCommandAcceptedComment
}

func (s *GoogleInboxService) validateCommandTargetSection(ctx context.Context, file google.DriveFile, command reviewAgentCommand) error {
	if command.mode != domain.AnalysisModeIncrementalReview || strings.TrimSpace(command.targetSectionTitle) == "" {
		return nil
	}
	if s.analysisService == nil || s.analysisService.documentReader == nil {
		return apperrors.New(apperrors.KindInternal, "google docs reader is not configured")
	}

	doc, err := s.analysisService.documentReader.Read(ctx, file.DocumentURL)
	if err != nil {
		return err
	}
	_, err = resolveTargetSections(toDomainSections(doc.Sections), "", command.targetSectionTitle)
	return err
}
