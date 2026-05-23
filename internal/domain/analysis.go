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
	ID           string
	DocumentID   string
	Mode         AnalysisMode
	Status       AnalysisStatus
	Provider     string
	Model        string
	ChunkCount   int
	Findings     []Finding
	Chunks       []AnalysisChunk
	Summary      string
	ErrorMessage string
	CreatedAt    time.Time
	CompletedAt  *time.Time
}

type AnalysisMode string

const (
	AnalysisModeFullReview        AnalysisMode = "full_review"
	AnalysisModeIncrementalReview AnalysisMode = "incremental_review"
)

type Finding struct {
	ChunkIndex  int
	Role        string
	Category    string
	Severity    Severity
	Problem     string
	WhyItIsBad  string
	HowToFix    string
	SourceChunk string
}

type AnalysisChunk struct {
	ID             string
	AnalysisID     string
	ChunkIndex     int
	ChunkText      string
	PromptVersion  string
	SystemPrompt   string
	UserPrompt     string
	RawLLMResponse string
	CreatedAt      time.Time
}
