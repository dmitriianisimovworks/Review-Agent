package domain

import "time"

type AnalysisStatus string

const (
	AnalysisStatusQueued     AnalysisStatus = "queued"
	AnalysisStatusProcessing AnalysisStatus = "processing"
	AnalysisStatusCompleted  AnalysisStatus = "completed"
)

type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityWarning  Severity = "WARNING"
	SeverityError    Severity = "ERROR"
	SeverityCritical Severity = "CRITICAL"
)

type Analysis struct {
	ID          string
	DocumentID  string
	Status      AnalysisStatus
	Findings    []Finding
	Summary     string
	CreatedAt   time.Time
	CompletedAt *time.Time
}

type Finding struct {
	Role        string
	Category    string
	Severity    Severity
	Problem     string
	WhyItIsBad  string
	HowToFix    string
	SourceChunk string
}
