package domain

import "time"

type TrackedDocument struct {
	ID          string
	Source      DocumentSource
	ExternalID  string
	Name        string
	DocumentURL string
	CreatedAt   time.Time
}
