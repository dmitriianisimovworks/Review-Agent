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
	files []google.DriveFile
}

func (w stubFolderWatcher) ListDocuments(context.Context, string) ([]google.DriveFile, error) {
	return w.files, nil
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

	inbox := NewGoogleInboxService("folder", time.Minute, stubFolderWatcher{}, documentRepo, analysisService, commentService, stubCommentReader{}, publisher)
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
