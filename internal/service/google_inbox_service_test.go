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

	inbox := NewGoogleInboxService("folder", time.Minute, stubFolderWatcher{}, documentRepo, analysisService, commentService, publisher)
	inbox.pending["analysis_1"] = inboxPendingAnalysis{
		documentExternalID: "google-doc-1",
		startedAt:          time.Now().Add(-inboxProgressCommentDelay - time.Second),
		serviceCommentIDs:  []string{"service-1"},
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
	if len(pending.serviceCommentIDs) != 2 {
		t.Fatalf("expected service comments to include delayed comment id, got %+v", pending.serviceCommentIDs)
	}
}

func TestGoogleInboxServiceCleansUpServiceCommentsAfterFinalPublish(t *testing.T) {
	documentRepo := &stubDocumentRepo{
		got: domain.Document{
			ID:         "doc_1",
			Source:     domain.DocumentSourceGoogleDocs,
			ExternalID: "google-doc-1",
		},
	}
	completedAt := time.Now().UTC()
	analysisRepo := &stubAnalysisRepo{
		got: domain.Analysis{
			ID:          "analysis_1",
			DocumentID:  "doc_1",
			Status:      domain.AnalysisStatusCompleted,
			CompletedAt: &completedAt,
		},
	}
	publisher := &stubCommentPublisher{
		publishIDs: []string{"final-1"},
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
			InlineComments:  false,
			SummaryComments: true,
		}},
	)

	inbox := NewGoogleInboxService("folder", time.Minute, stubFolderWatcher{}, documentRepo, analysisService, commentService, publisher)
	inbox.pending["analysis_1"] = inboxPendingAnalysis{
		documentExternalID: "google-doc-1",
		startedAt:          time.Now().Add(-time.Minute),
		serviceCommentIDs:  []string{"service-1", "service-2"},
	}

	inbox.processPending(context.Background())

	if _, exists := inbox.pending["analysis_1"]; exists {
		t.Fatalf("expected pending analysis to be removed after final publish")
	}
	if publisher.deletedDocumentID != "google-doc-1" {
		t.Fatalf("expected cleanup for google-doc-1, got %s", publisher.deletedDocumentID)
	}
	if len(publisher.deletedCommentIDs) != 2 {
		t.Fatalf("expected two service comments to be deleted, got %+v", publisher.deletedCommentIDs)
	}
	if len(publisher.publishedBatches) != 1 {
		t.Fatalf("expected one final publish batch, got %d", len(publisher.publishedBatches))
	}
	if publisher.publishedBatches[0][0].Content != "ok" {
		t.Fatalf("expected final publish payload from formatter, got %+v", publisher.publishedBatches[0])
	}
}
