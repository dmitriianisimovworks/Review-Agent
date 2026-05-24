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
	ReviewKey         string
	RawContent        string
	NormalizedContent string
	Sections          []DocumentSection
	Blocks            []DocumentBlock
	CreatedAt         time.Time
}

type DocumentRange struct {
	StartIndex int64 `json:"start_index"`
	EndIndex   int64 `json:"end_index"`
}

type DocumentSection struct {
	ID      string        `json:"id"`
	Title   string        `json:"title"`
	Level   int           `json:"level"`
	Range   DocumentRange `json:"range"`
	Content string        `json:"content"`
}

type DocumentBlock struct {
	Kind         string        `json:"kind"`
	Text         string        `json:"text"`
	Range        DocumentRange `json:"range"`
	HeadingLevel int           `json:"heading_level,omitempty"`
	ListLevel    int           `json:"list_level,omitempty"`
	SectionID    string        `json:"section_id,omitempty"`
	SectionTitle string        `json:"section_title,omitempty"`
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
