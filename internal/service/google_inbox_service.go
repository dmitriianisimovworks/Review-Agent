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

const inboxProgressCommentDelay = 20 * time.Second
const inboxTrackedDiscoveryInterval = 30 * time.Second

const (
	reviewAgentCommandPrefix             = "@review-agent"
	reviewAgentCommandFull               = "full"
	reviewAgentCommandIncremental        = "incremental"
	reviewAgentCommandIncrementalSection = "incremental section:"
	reviewAgentCommandCleanup            = "cleanup"
)

type reviewAgentCommand struct {
	commentID          string
	kind               string
	mode               domain.AnalysisMode
	targetSectionTitle string
	createdAt          time.Time
}

type inboxPendingAnalysis struct {
	documentExternalID  string
	lastBusyCommandID   string
	startedAt           time.Time
	progressCommentSent bool
}

type GoogleInboxService struct {
	folderID         string
	pollInterval     time.Duration
	watcher          google.FolderWatcher
	trackedRepo      repository.TrackedDocumentRepository
	documentRepo     repository.DocumentRepository
	analysisService  *AnalysisService
	commentService   *CommentService
	commentReader    google.CommentReader
	commentPublisher google.CommentPublisher

	mu                     sync.Mutex
	pending                map[string]inboxPendingAnalysis
	lastTrackedDiscoveryAt time.Time
}

func NewGoogleInboxService(
	folderID string,
	pollInterval time.Duration,
	watcher google.FolderWatcher,
	trackedRepo repository.TrackedDocumentRepository,
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
		trackedRepo:      trackedRepo,
		documentRepo:     documentRepo,
		analysisService:  analysisService,
		commentService:   commentService,
		commentReader:    commentReader,
		commentPublisher: commentPublisher,
		pending:          make(map[string]inboxPendingAnalysis),
	}
}

func (s *GoogleInboxService) Enabled() bool {
	if s == nil || s.analysisService == nil || s.commentService == nil || s.commentReader == nil || s.commentPublisher == nil {
		return false
	}
	hasFolderPolling := s.folderID != "" && s.watcher != nil
	hasTrackedPolling := s.trackedRepo != nil
	return hasFolderPolling || hasTrackedPolling
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
	if err := s.syncAccessibleTrackedDocuments(ctx); err != nil {
		return err
	}
	folderFiles, trackedFiles, err := s.listPolledDocuments(ctx)
	if err != nil {
		return err
	}
	if err := s.enqueueNewDocuments(ctx, folderFiles); err != nil {
		return err
	}
	if err := s.processCommandComments(ctx, mergeDriveFiles(folderFiles, trackedFiles)); err != nil {
		return err
	}
	s.processPending(ctx)
	return nil
}

func (s *GoogleInboxService) syncAccessibleTrackedDocuments(ctx context.Context) error {
	if s.trackedRepo == nil {
		return nil
	}
	discoveryWatcher, ok := s.watcher.(google.AccessibleDocumentWatcher)
	if !ok || discoveryWatcher == nil {
		return nil
	}
	if !s.shouldRunTrackedDiscovery() {
		return nil
	}

	files, err := discoveryWatcher.ListAccessibleDocuments(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, file := range files {
		if strings.TrimSpace(file.ID) == "" || strings.TrimSpace(file.DocumentURL) == "" {
			continue
		}
		if err := s.trackedRepo.Save(ctx, domain.TrackedDocument{
			ID:          fmt.Sprintf("tracked_doc_%s", strings.TrimSpace(file.ID)),
			Source:      domain.DocumentSourceGoogleDocs,
			ExternalID:  strings.TrimSpace(file.ID),
			Name:        strings.TrimSpace(file.Name),
			DocumentURL: strings.TrimSpace(file.DocumentURL),
			CreatedAt:   now,
		}); err != nil {
			return err
		}
	}
	s.markTrackedDiscoveryRun(now)
	return nil
}

func (s *GoogleInboxService) listPolledDocuments(ctx context.Context) ([]google.DriveFile, []google.DriveFile, error) {
	folderFiles := make([]google.DriveFile, 0)
	if s.folderID != "" && s.watcher != nil {
		files, err := s.watcher.ListDocuments(ctx, s.folderID)
		if err != nil {
			return nil, nil, err
		}
		folderFiles = files
	}

	trackedFiles := make([]google.DriveFile, 0)
	if s.trackedRepo != nil {
		items, err := s.trackedRepo.ListBySource(ctx, domain.DocumentSourceGoogleDocs)
		if err != nil {
			return nil, nil, err
		}
		trackedFiles = make([]google.DriveFile, 0, len(items))
		for _, item := range items {
			if strings.TrimSpace(item.ExternalID) == "" || strings.TrimSpace(item.DocumentURL) == "" {
				continue
			}
			trackedFiles = append(trackedFiles, google.DriveFile{
				ID:          strings.TrimSpace(item.ExternalID),
				Name:        strings.TrimSpace(item.Name),
				DocumentURL: strings.TrimSpace(item.DocumentURL),
			})
		}
	}

	return folderFiles, trackedFiles, nil
}

func (s *GoogleInboxService) enqueueNewDocuments(ctx context.Context, files []google.DriveFile) error {
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
		if s.isPendingDocument(file.ID) {
			if s.shouldNotifyBusyCommand(file.ID, command.commentID) {
				s.markBusyCommandNotified(file.ID, command.commentID)
			}
			continue
		}

		if err := s.cleanupAgentComments(ctx, file.ID); err != nil {
			log.Printf("google inbox: document_id=%q cleanup agent comments: %v", file.ID, err)
		}
		if command.kind == reviewAgentCommandCleanup {
			if err := s.cleanupAgentComments(ctx, file.ID); err != nil {
				return err
			}
			continue
		}
		if err := s.validateCommandTargetSection(ctx, file, command); err != nil {
			log.Printf("google inbox: document_id=%q target section not found: %q", file.ID, strings.TrimSpace(command.targetSectionTitle))
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
				s.markProgressCommentSent(analysisID)
			}
		}
	}
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
		command, ok := parseReviewAgentCommand(comment.ID, comment.Content, comment.CreatedAt)
		if ok {
			return command, true, nil
		}
	}
	return reviewAgentCommand{}, false, nil
}

func (s *GoogleInboxService) cleanupAgentComments(ctx context.Context, documentExternalID string) error {
	comments, err := s.commentReader.List(ctx, documentExternalID)
	if err != nil {
		return err
	}

	ids := make([]string, 0)
	for _, comment := range comments {
		if !comment.AuthorIsMe || comment.ID == "" {
			continue
		}
		if isReviewAgentCommandText(comment.Content) {
			continue
		}
		ids = append(ids, comment.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	return s.commentPublisher.Delete(ctx, documentExternalID, ids)
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
		lastBusyCommandID:   "",
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

func (s *GoogleInboxService) shouldRunTrackedDiscovery() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastTrackedDiscoveryAt.IsZero() || time.Since(s.lastTrackedDiscoveryAt) >= inboxTrackedDiscoveryInterval
}

func (s *GoogleInboxService) markTrackedDiscoveryRun(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastTrackedDiscoveryAt = now
}

func parseReviewAgentCommand(commentID, content string, createdAtUnixNano int64) (reviewAgentCommand, bool) {
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
	case commandText == "":
		return reviewAgentCommand{commentID: commentID, kind: reviewAgentCommandFull, mode: domain.AnalysisModeFullReview, createdAt: createdAt}, true
	case commandText == reviewAgentCommandFull:
		return reviewAgentCommand{commentID: commentID, kind: reviewAgentCommandFull, mode: domain.AnalysisModeFullReview, createdAt: createdAt}, true
	case commandText == reviewAgentCommandCleanup:
		return reviewAgentCommand{commentID: commentID, kind: reviewAgentCommandCleanup, createdAt: createdAt}, true
	case commandText == reviewAgentCommandIncremental:
		return reviewAgentCommand{commentID: commentID, kind: reviewAgentCommandIncremental, mode: domain.AnalysisModeIncrementalReview, createdAt: createdAt}, true
	case strings.HasPrefix(commandText, reviewAgentCommandIncrementalSection):
		section := strings.TrimSpace(strings.TrimPrefix(commandText, reviewAgentCommandIncrementalSection))
		if section == "" {
			return reviewAgentCommand{}, false
		}
		return reviewAgentCommand{
			commentID:          commentID,
			kind:               reviewAgentCommandIncrementalSection,
			mode:               domain.AnalysisModeIncrementalReview,
			targetSectionTitle: section,
			createdAt:          createdAt,
		}, true
	default:
		return reviewAgentCommand{}, false
	}
}

func mergeDriveFiles(groups ...[]google.DriveFile) []google.DriveFile {
	seen := make(map[string]struct{})
	result := make([]google.DriveFile, 0)
	for _, group := range groups {
		for _, file := range group {
			id := strings.TrimSpace(file.ID)
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, file)
		}
	}
	return result
}

func isReviewAgentCommandText(content string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(content)), reviewAgentCommandPrefix)
}

func reviewKeyForGoogleDoc(documentExternalID string) string {
	return string(domain.DocumentSourceGoogleDocs) + ":" + strings.TrimSpace(strings.ToLower(documentExternalID))
}

func (s *GoogleInboxService) shouldNotifyBusyCommand(documentExternalID, commandID string) bool {
	if strings.TrimSpace(commandID) == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pending := range s.pending {
		if pending.documentExternalID != documentExternalID {
			continue
		}
		return pending.lastBusyCommandID != commandID
	}
	return false
}

func (s *GoogleInboxService) markBusyCommandNotified(documentExternalID, commandID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for analysisID, pending := range s.pending {
		if pending.documentExternalID != documentExternalID {
			continue
		}
		pending.lastBusyCommandID = commandID
		s.pending[analysisID] = pending
		return
	}
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
