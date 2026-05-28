package service

import (
	"context"
	"testing"
	"time"

	"technical-specification-review-agent/internal/config"
	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/google"
	"technical-specification-review-agent/internal/reviewconfig"
)

type stubFolderWatcher struct {
	files           []google.DriveFile
	accessibleFiles []google.DriveFile
}

func (w stubFolderWatcher) ListDocuments(context.Context, string) ([]google.DriveFile, error) {
	return w.files, nil
}

func (w stubFolderWatcher) ListAccessibleDocuments(context.Context) ([]google.DriveFile, error) {
	return w.accessibleFiles, nil
}

type stubCommentReader struct {
	comments map[string][]google.Comment
}

func (r stubCommentReader) List(_ context.Context, documentExternalID string) ([]google.Comment, error) {
	return r.comments[documentExternalID], nil
}

func TestGoogleInboxServicePublishesDelayedProgressComment(t *testing.T) {
	documentRepo := &stubDocumentRepo{}
	analysisRepo := &stubAnalysisRepo{
		got: domain.Analysis{
			ID:         "analysis_1",
			DocumentID: "doc_1",
			Status:     domain.AnalysisStatusProcessing,
		},
	}
	publisher := &stubCommentPublisher{
		publishIDs: []string{"service-1", "service-2"},
	}

	analysisService := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		&recordingLLMClient{},
		nil,
		publisher,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{settings: reviewconfig.Settings{}},
		nil,
	)
	commentService := NewCommentService(
		documentRepo,
		analysisRepo,
		&recordingFormatter{},
		publisher,
		stubReviewConfigProvider{settings: reviewconfig.Settings{
			InlineComments:  true,
			SummaryComments: true,
		}},
	)

	inbox := NewGoogleInboxService("folder", time.Minute, stubFolderWatcher{}, nil, documentRepo, analysisService, commentService, stubCommentReader{}, publisher)
	inbox.pending["analysis_1"] = inboxPendingAnalysis{
		documentExternalID: "google-doc-1",
		startedAt:          time.Now().Add(-inboxProgressCommentDelay - time.Second),
	}

	inbox.processPending(context.Background())

	pending := inbox.pending["analysis_1"]
	if !pending.progressCommentSent {
		t.Fatalf("expected delayed progress comment to be marked as sent")
	}
	if len(publisher.publishedBatches) != 1 {
		t.Fatalf("expected one additional publish call, got %d", len(publisher.publishedBatches))
	}
	if got := publisher.publishedBatches[0][0].Content; got != inboxProgressComment {
		t.Fatalf("expected progress comment content %q, got %q", inboxProgressComment, got)
	}
}

func TestGoogleInboxServiceQueuesIncrementalSectionCommandFromComment(t *testing.T) {
	documentRepo := &stubDocumentRepo{
		got: domain.Document{
			ID:         "doc_existing",
			Source:     domain.DocumentSourceGoogleDocs,
			ExternalID: "google-doc-1",
			ReviewKey:  "google_docs:google-doc-1",
			Name:       "Billing Spec",
		},
	}
	analysisRepo := &stubAnalysisRepo{
		prior: []domain.Analysis{{
			ID:        "analysis_prev",
			CreatedAt: time.Now().Add(-time.Hour),
			Status:    domain.AnalysisStatusCompleted,
		}},
	}
	llmClient := &recordingLLMClient{}
	jobRunner := &stubJobRunner{}
	publisher := &stubCommentPublisher{
		publishIDs: []string{"service-1"},
	}
	commentReader := stubCommentReader{
		comments: map[string][]google.Comment{
			"google-doc-1": {{
				ID:        "comment-1",
				Content:   "@review-agent incremental section: 4.3",
				CreatedAt: time.Now().UTC().UnixNano(),
			}},
		},
	}

	analysisService := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		llmClient,
		&stubDocumentReader{document: google.Document{
			ExternalID: "google-doc-1",
			Title:      "Billing Spec",
			Content:    "4.3 Refund and Adjustment Approval\ncontent",
			Sections: []google.Section{{
				ID:      "refund",
				Title:   "4.3 Refund and Adjustment Approval",
				Level:   1,
				Content: "content",
			}},
		}},
		publisher,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{settings: reviewconfig.Settings{
			Roles:           domain.DefaultReviewerRoles(),
			InlineComments:  true,
			SummaryComments: true,
			MemoryEnabled:   true,
			ChunkSize:       5000,
			MaxChunks:       10,
		}},
		jobRunner,
	)
	commentService := NewCommentService(
		documentRepo,
		analysisRepo,
		&recordingFormatter{},
		publisher,
		stubReviewConfigProvider{settings: reviewconfig.Settings{
			InlineComments:  true,
			SummaryComments: true,
		}},
	)

	inbox := NewGoogleInboxService(
		"folder",
		time.Minute,
		stubFolderWatcher{files: []google.DriveFile{{
			ID:          "google-doc-1",
			Name:        "Billing Spec",
			DocumentURL: "https://docs.google.com/document/d/google-doc-1/edit",
		}}},
		nil,
		documentRepo,
		analysisService,
		commentService,
		commentReader,
		publisher,
	)

	if err := inbox.tick(context.Background()); err != nil {
		t.Fatalf("tick() error = %v", err)
	}
	if len(jobRunner.enqueued) != 1 {
		t.Fatalf("expected one enqueued analysis, got %+v", jobRunner.enqueued)
	}
	if analysisRepo.saved.Mode != domain.AnalysisModeIncrementalReview {
		t.Fatalf("expected incremental review, got %s", analysisRepo.saved.Mode)
	}
	if analysisRepo.saved.TargetSectionTitle != "4.3" {
		t.Fatalf("expected target section 4.3, got %q", analysisRepo.saved.TargetSectionTitle)
	}
	if len(publisher.publishedBatches) != 1 {
		t.Fatalf("expected one service publish batch, got %d", len(publisher.publishedBatches))
	}
	if got := publisher.publishedBatches[0][0].Content; got != "Команда принята. Запускаю incremental review для раздела 4.3." {
		t.Fatalf("expected command ack comment, got %q", got)
	}
}

func TestGoogleInboxServiceCleansUpAgentCommentsBeforeCommandRun(t *testing.T) {
	documentRepo := &stubDocumentRepo{
		got: domain.Document{
			ID:         "doc_existing",
			Source:     domain.DocumentSourceGoogleDocs,
			ExternalID: "google-doc-1",
			ReviewKey:  "google_docs:google-doc-1",
			Name:       "Billing Spec",
		},
	}
	analysisRepo := &stubAnalysisRepo{
		prior: []domain.Analysis{{
			ID:        "analysis_prev",
			CreatedAt: time.Now().Add(-time.Hour),
			Status:    domain.AnalysisStatusCompleted,
		}},
	}
	jobRunner := &stubJobRunner{}
	publisher := &stubCommentPublisher{
		publishIDs: []string{"service-1"},
	}
	commentReader := stubCommentReader{
		comments: map[string][]google.Comment{
			"google-doc-1": {
				{
					ID:         "user-command",
					Content:    "@review-agent incremental",
					CreatedAt:  time.Now().UTC().UnixNano(),
					AuthorIsMe: false,
				},
				{
					ID:         "agent-summary",
					Content:    "Итоговый комментарий",
					CreatedAt:  time.Now().Add(-time.Minute).UTC().UnixNano(),
					AuthorIsMe: true,
				},
				{
					ID:         "agent-role",
					Content:    "🧭 Tech Lead",
					CreatedAt:  time.Now().Add(-time.Minute).UTC().UnixNano(),
					AuthorIsMe: true,
				},
			},
		},
	}

	analysisService := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		&recordingLLMClient{},
		nil,
		publisher,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{settings: reviewconfig.Settings{
			Roles:           domain.DefaultReviewerRoles(),
			InlineComments:  true,
			SummaryComments: true,
			MemoryEnabled:   true,
			ChunkSize:       5000,
			MaxChunks:       10,
		}},
		jobRunner,
	)
	commentService := NewCommentService(
		documentRepo,
		analysisRepo,
		&recordingFormatter{},
		publisher,
		stubReviewConfigProvider{settings: reviewconfig.Settings{
			InlineComments:  true,
			SummaryComments: true,
		}},
	)

	inbox := NewGoogleInboxService(
		"folder",
		time.Minute,
		stubFolderWatcher{files: []google.DriveFile{{
			ID:          "google-doc-1",
			Name:        "Billing Spec",
			DocumentURL: "https://docs.google.com/document/d/google-doc-1/edit",
		}}},
		nil,
		documentRepo,
		analysisService,
		commentService,
		commentReader,
		publisher,
	)

	if err := inbox.tick(context.Background()); err != nil {
		t.Fatalf("tick() error = %v", err)
	}
	if len(jobRunner.enqueued) != 1 {
		t.Fatalf("expected one enqueued analysis, got %+v", jobRunner.enqueued)
	}
	if publisher.deletedDocumentID != "google-doc-1" {
		t.Fatalf("expected cleanup for google-doc-1, got %q", publisher.deletedDocumentID)
	}
	if len(publisher.deletedCommentIDs) != 2 {
		t.Fatalf("expected two agent comments to be deleted, got %+v", publisher.deletedCommentIDs)
	}
	for _, id := range publisher.deletedCommentIDs {
		if id == "user-command" {
			t.Fatalf("cleanup should not delete user command comment")
		}
	}
}

func TestGoogleInboxServiceCleanupCommandDeletesAgentCommentsWithoutStartingAnalysis(t *testing.T) {
	documentRepo := &stubDocumentRepo{
		got: domain.Document{
			ID:         "doc_existing",
			Source:     domain.DocumentSourceGoogleDocs,
			ExternalID: "google-doc-1",
			ReviewKey:  "google_docs:google-doc-1",
			Name:       "Billing Spec",
		},
	}
	analysisRepo := &stubAnalysisRepo{
		prior: []domain.Analysis{{
			ID:        "analysis_prev",
			CreatedAt: time.Now().Add(-time.Hour),
			Status:    domain.AnalysisStatusCompleted,
		}},
	}
	jobRunner := &stubJobRunner{}
	publisher := &stubCommentPublisher{
		publishIDs: []string{"service-1", "service-2"},
	}
	commentReader := stubCommentReader{
		comments: map[string][]google.Comment{
			"google-doc-1": {
				{
					ID:         "user-command",
					Content:    "@review-agent cleanup",
					CreatedAt:  time.Now().UTC().UnixNano(),
					AuthorIsMe: false,
				},
				{
					ID:         "agent-summary",
					Content:    "Итоговый комментарий",
					CreatedAt:  time.Now().Add(-time.Minute).UTC().UnixNano(),
					AuthorIsMe: true,
				},
				{
					ID:         "agent-role",
					Content:    "🧭 Tech Lead",
					CreatedAt:  time.Now().Add(-time.Minute).UTC().UnixNano(),
					AuthorIsMe: true,
				},
			},
		},
	}

	analysisService := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		&recordingLLMClient{},
		nil,
		publisher,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{settings: reviewconfig.Settings{}},
		jobRunner,
	)
	commentService := NewCommentService(
		documentRepo,
		analysisRepo,
		&recordingFormatter{},
		publisher,
		stubReviewConfigProvider{settings: reviewconfig.Settings{
			InlineComments:  true,
			SummaryComments: true,
		}},
	)

	inbox := NewGoogleInboxService(
		"folder",
		time.Minute,
		stubFolderWatcher{files: []google.DriveFile{{
			ID:          "google-doc-1",
			Name:        "Billing Spec",
			DocumentURL: "https://docs.google.com/document/d/google-doc-1/edit",
		}}},
		nil,
		documentRepo,
		analysisService,
		commentService,
		commentReader,
		publisher,
	)

	if err := inbox.tick(context.Background()); err != nil {
		t.Fatalf("tick() error = %v", err)
	}
	if len(jobRunner.enqueued) != 0 {
		t.Fatalf("expected no enqueued analysis, got %+v", jobRunner.enqueued)
	}
	if publisher.deletedDocumentID != "google-doc-1" {
		t.Fatalf("expected cleanup for google-doc-1, got %q", publisher.deletedDocumentID)
	}
	if len(publisher.deletedCommentIDs) != 2 {
		t.Fatalf("expected two agent comments to be deleted, got %+v", publisher.deletedCommentIDs)
	}
	if len(publisher.publishedBatches) != 2 {
		t.Fatalf("expected two service publish batches, got %d", len(publisher.publishedBatches))
	}
	if got := publisher.publishedBatches[0][0].Content; got != inboxCleanupCommandAcceptedComment {
		t.Fatalf("expected cleanup ack comment, got %q", got)
	}
	if got := publisher.publishedBatches[1][0].Content; got != inboxCleanupCompletedComment {
		t.Fatalf("expected cleanup completed comment, got %q", got)
	}
}

func TestGoogleInboxServicePublishesAlreadyProcessingCommentForCommand(t *testing.T) {
	documentRepo := &stubDocumentRepo{
		got: domain.Document{
			ID:         "doc_existing",
			Source:     domain.DocumentSourceGoogleDocs,
			ExternalID: "google-doc-1",
			ReviewKey:  "google_docs:google-doc-1",
			Name:       "Billing Spec",
		},
	}
	analysisRepo := &stubAnalysisRepo{
		prior: []domain.Analysis{{
			ID:        "analysis_prev",
			CreatedAt: time.Now().Add(-time.Hour),
			Status:    domain.AnalysisStatusCompleted,
		}},
	}
	publisher := &stubCommentPublisher{
		publishIDs: []string{"service-1"},
	}
	commentReader := stubCommentReader{
		comments: map[string][]google.Comment{
			"google-doc-1": {{
				ID:        "comment-1",
				Content:   "@review-agent incremental",
				CreatedAt: time.Now().UTC().UnixNano(),
			}},
		},
	}

	analysisService := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		&recordingLLMClient{},
		nil,
		publisher,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{settings: reviewconfig.Settings{}},
		&stubJobRunner{},
	)
	commentService := NewCommentService(
		documentRepo,
		analysisRepo,
		&recordingFormatter{},
		publisher,
		stubReviewConfigProvider{settings: reviewconfig.Settings{
			InlineComments:  true,
			SummaryComments: true,
		}},
	)

	inbox := NewGoogleInboxService(
		"folder",
		time.Minute,
		stubFolderWatcher{files: []google.DriveFile{{
			ID:          "google-doc-1",
			Name:        "Billing Spec",
			DocumentURL: "https://docs.google.com/document/d/google-doc-1/edit",
		}}},
		nil,
		documentRepo,
		analysisService,
		commentService,
		commentReader,
		publisher,
	)
	inbox.pending["analysis_running"] = inboxPendingAnalysis{
		documentExternalID: "google-doc-1",
		startedAt:          time.Now().Add(-time.Second),
	}

	if err := inbox.tick(context.Background()); err != nil {
		t.Fatalf("tick() error = %v", err)
	}
	if err := inbox.tick(context.Background()); err != nil {
		t.Fatalf("second tick() error = %v", err)
	}
	if len(publisher.publishedBatches) != 1 {
		t.Fatalf("expected one publish batch, got %d", len(publisher.publishedBatches))
	}
	if got := publisher.publishedBatches[0][0].Content; got != inboxAlreadyProcessingComment {
		t.Fatalf("expected already-processing comment, got %q", got)
	}
}

func TestGoogleInboxServicePublishesSectionNotFoundComment(t *testing.T) {
	documentRepo := &stubDocumentRepo{
		got: domain.Document{
			ID:         "doc_existing",
			Source:     domain.DocumentSourceGoogleDocs,
			ExternalID: "google-doc-1",
			ReviewKey:  "google_docs:google-doc-1",
			Name:       "Billing Spec",
		},
	}
	analysisRepo := &stubAnalysisRepo{
		prior: []domain.Analysis{{
			ID:        "analysis_prev",
			CreatedAt: time.Now().Add(-time.Hour),
			Status:    domain.AnalysisStatusCompleted,
		}},
	}
	publisher := &stubCommentPublisher{
		publishIDs: []string{"service-1", "service-2"},
	}
	commentReader := stubCommentReader{
		comments: map[string][]google.Comment{
			"google-doc-1": {{
				ID:        "comment-1",
				Content:   "@review-agent incremental section: 9.9",
				CreatedAt: time.Now().UTC().UnixNano(),
			}},
		},
	}
	analysisService := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		&recordingLLMClient{},
		&stubDocumentReader{document: google.Document{
			ExternalID: "google-doc-1",
			Title:      "Billing Spec",
			Content:    "4.3 Refund and Adjustment Approval\ncontent",
			Sections: []google.Section{{
				ID:      "refund",
				Title:   "4.3 Refund and Adjustment Approval",
				Level:   1,
				Content: "content",
			}},
		}},
		publisher,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{settings: reviewconfig.Settings{
			Roles:           []domain.ReviewerRole{domain.ReviewerRoleTechLead},
			InlineComments:  true,
			SummaryComments: true,
			MemoryEnabled:   true,
			ChunkSize:       5000,
			MaxChunks:       10,
		}},
		&stubJobRunner{},
	)
	commentService := NewCommentService(
		documentRepo,
		analysisRepo,
		&recordingFormatter{},
		publisher,
		stubReviewConfigProvider{settings: reviewconfig.Settings{
			InlineComments:  true,
			SummaryComments: true,
		}},
	)

	inbox := NewGoogleInboxService(
		"folder",
		time.Minute,
		stubFolderWatcher{files: []google.DriveFile{{
			ID:          "google-doc-1",
			Name:        "Billing Spec",
			DocumentURL: "https://docs.google.com/document/d/google-doc-1/edit",
		}}},
		nil,
		documentRepo,
		analysisService,
		commentService,
		commentReader,
		publisher,
	)

	if err := inbox.tick(context.Background()); err != nil {
		t.Fatalf("tick() error = %v", err)
	}
	if len(publisher.publishedBatches) != 2 {
		t.Fatalf("expected two publish batches, got %d", len(publisher.publishedBatches))
	}
	if got := publisher.publishedBatches[0][0].Content; got != "Команда принята. Запускаю incremental review для раздела 9.9." {
		t.Fatalf("expected command ack comment, got %q", got)
	}
	if got := publisher.publishedBatches[1][0].Content; got != "Команда принята, но раздел 9.9 не найден в структуре документа." {
		t.Fatalf("expected section-not-found comment, got %q", got)
	}
}

func TestGoogleInboxServiceQueuesCommandForTrackedDocumentWithoutInboxFolder(t *testing.T) {
	documentRepo := &stubDocumentRepo{}
	analysisRepo := &stubAnalysisRepo{}
	jobRunner := &stubJobRunner{}
	publisher := &stubCommentPublisher{
		publishIDs: []string{"service-1"},
	}
	commentReader := stubCommentReader{
		comments: map[string][]google.Comment{
			"google-doc-2": {{
				ID:         "comment-1",
				Content:    "@review-agent",
				CreatedAt:  time.Now().UTC().UnixNano(),
				AuthorIsMe: false,
			}},
		},
	}
	trackedRepo := &stubTrackedDocumentRepo{
		entries: []domain.TrackedDocument{{
			ID:          "tracked-1",
			Source:      domain.DocumentSourceGoogleDocs,
			ExternalID:  "google-doc-2",
			Name:        "Reviewer Spec",
			DocumentURL: "https://docs.google.com/document/d/google-doc-2/edit",
			CreatedAt:   time.Now().Add(-time.Minute).UTC(),
		}},
	}

	analysisService := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		&recordingLLMClient{},
		nil,
		publisher,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{settings: reviewconfig.Settings{}},
		jobRunner,
	)
	commentService := NewCommentService(
		documentRepo,
		analysisRepo,
		&recordingFormatter{},
		publisher,
		stubReviewConfigProvider{settings: reviewconfig.Settings{
			InlineComments:  true,
			SummaryComments: true,
		}},
	)

	inbox := NewGoogleInboxService(
		"",
		time.Minute,
		nil,
		trackedRepo,
		documentRepo,
		analysisService,
		commentService,
		commentReader,
		publisher,
	)

	if !inbox.Enabled() {
		t.Fatalf("expected inbox to be enabled for tracked docs polling")
	}
	if err := inbox.tick(context.Background()); err != nil {
		t.Fatalf("tick() error = %v", err)
	}
	if len(jobRunner.enqueued) != 1 {
		t.Fatalf("expected one enqueued analysis, got %+v", jobRunner.enqueued)
	}
	if analysisRepo.saved.Mode != domain.AnalysisModeFullReview {
		t.Fatalf("expected full review, got %s", analysisRepo.saved.Mode)
	}
	if got := analysisRepo.saved.DocumentID; got == "" {
		t.Fatalf("expected queued analysis document id to be set")
	}
	if len(publisher.publishedBatches) != 1 {
		t.Fatalf("expected one command ack publish batch, got %d", len(publisher.publishedBatches))
	}
	if got := publisher.publishedBatches[0][0].Content; got != inboxFullCommandAcceptedComment {
		t.Fatalf("expected full-review ack comment, got %q", got)
	}
}

func TestGoogleInboxServiceAutoRegistersAccessibleGoogleDocs(t *testing.T) {
	documentRepo := &stubDocumentRepo{}
	analysisRepo := &stubAnalysisRepo{}
	jobRunner := &stubJobRunner{}
	publisher := &stubCommentPublisher{
		publishIDs: []string{"service-1"},
	}
	commentReader := stubCommentReader{
		comments: map[string][]google.Comment{
			"google-doc-3": {{
				ID:         "comment-1",
				Content:    "@review-agent",
				CreatedAt:  time.Now().UTC().UnixNano(),
				AuthorIsMe: false,
			}},
		},
	}
	trackedRepo := &stubTrackedDocumentRepo{}
	watcher := stubFolderWatcher{
		accessibleFiles: []google.DriveFile{{
			ID:          "google-doc-3",
			Name:        "Accessible Spec",
			DocumentURL: "https://docs.google.com/document/d/google-doc-3/edit",
		}},
	}

	analysisService := NewAnalysisService(
		documentRepo,
		analysisRepo,
		stubAnalysisCache{},
		&recordingLLMClient{},
		nil,
		publisher,
		"openai_compatible",
		"test-model",
		config.DocumentConfig{ChunkSize: 5000, MaxChunks: 10},
		stubReviewConfigProvider{settings: reviewconfig.Settings{}},
		jobRunner,
	)
	commentService := NewCommentService(
		documentRepo,
		analysisRepo,
		&recordingFormatter{},
		publisher,
		stubReviewConfigProvider{settings: reviewconfig.Settings{
			InlineComments:  true,
			SummaryComments: true,
		}},
	)

	inbox := NewGoogleInboxService(
		"",
		time.Minute,
		watcher,
		trackedRepo,
		documentRepo,
		analysisService,
		commentService,
		commentReader,
		publisher,
	)

	if err := inbox.tick(context.Background()); err != nil {
		t.Fatalf("tick() error = %v", err)
	}
	if len(trackedRepo.entries) != 1 {
		t.Fatalf("expected one auto-registered tracked doc, got %+v", trackedRepo.entries)
	}
	if trackedRepo.entries[0].ExternalID != "google-doc-3" {
		t.Fatalf("expected tracked external id google-doc-3, got %q", trackedRepo.entries[0].ExternalID)
	}
	if len(jobRunner.enqueued) != 1 {
		t.Fatalf("expected one enqueued analysis, got %+v", jobRunner.enqueued)
	}
	if got := publisher.publishedBatches[0][0].Content; got != inboxFullCommandAcceptedComment {
		t.Fatalf("expected full-review ack comment, got %q", got)
	}
}
