package service

import (
	"context"
	"testing"

	"technical-specification-review-agent/internal/integration/google"
)

func TestTrackedDocumentServiceRegisterGoogleDoc(t *testing.T) {
	repo := &stubTrackedDocumentRepo{}
	reader := &stubDocumentReader{document: google.Document{
		ExternalID: "google-doc-1",
		Title:      "Reviewer Spec",
	}}
	service := NewTrackedDocumentService(repo, reader)

	tracked, err := service.RegisterGoogleDoc(context.Background(), RegisterTrackedGoogleDocInput{
		DocumentURL: "https://docs.google.com/document/d/google-doc-1/edit?tab=t.0",
	})
	if err != nil {
		t.Fatalf("RegisterGoogleDoc() error = %v", err)
	}
	if tracked.ExternalID != "google-doc-1" {
		t.Fatalf("expected external id google-doc-1, got %q", tracked.ExternalID)
	}
	if tracked.DocumentURL != "https://docs.google.com/document/d/google-doc-1/edit" {
		t.Fatalf("expected canonical doc url, got %q", tracked.DocumentURL)
	}
	if repo.saved.ExternalID != "google-doc-1" {
		t.Fatalf("expected tracked doc to be persisted, got %+v", repo.saved)
	}
}
