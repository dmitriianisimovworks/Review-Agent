package domain

import "time"

type DocumentSource string

const (
	DocumentSourceUpload     DocumentSource = "upload"
	DocumentSourceGoogleDocs DocumentSource = "google_docs"
)

type Document struct {
	ID         string
	Name       string
	Source     DocumentSource
	ExternalID string
	Content    string
	CreatedAt  time.Time
}
