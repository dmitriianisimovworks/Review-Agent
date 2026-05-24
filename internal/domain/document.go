package domain

import "time"

type DocumentSource string

const (
	DocumentSourceUpload     DocumentSource = "upload"
	DocumentSourceGoogleDocs DocumentSource = "google_docs"
)

type Document struct {
	ID                string
	Name              string
	Source            DocumentSource
	ExternalID        string
	RawContent        string
	NormalizedContent string
	CreatedAt         time.Time
}

type GoogleOAuthConnection struct {
	ID           string
	GoogleUserID string
	Email        string
	AccessToken  string
	RefreshToken string
	Expiry       *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
