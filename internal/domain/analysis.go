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
	Memory       ReviewMemory
	ErrorMessage string
	CreatedAt    time.Time
	CompletedAt  *time.Time
}

type ReviewerRole string

const (
	ReviewerRoleTechLead               ReviewerRole = "tech_lead"
	ReviewerRoleSolutionArchitect      ReviewerRole = "solution_architect"
	ReviewerRoleSeniorBackendEngineer  ReviewerRole = "senior_backend_engineer"
	ReviewerRoleSeniorFrontendEngineer ReviewerRole = "senior_frontend_engineer"
	ReviewerRoleDevOpsReviewer         ReviewerRole = "devops_reviewer"
	ReviewerRoleQAReviewer             ReviewerRole = "qa_reviewer"
)

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
	Role           ReviewerRole
	ChunkIndex     int
	ChunkText      string
	PromptVersion  string
	SystemPrompt   string
	UserPrompt     string
	RawLLMResponse string
	CreatedAt      time.Time
}

func DefaultReviewerRoles() []ReviewerRole {
	return []ReviewerRole{
		ReviewerRoleTechLead,
		ReviewerRoleSolutionArchitect,
		ReviewerRoleSeniorBackendEngineer,
		ReviewerRoleSeniorFrontendEngineer,
		ReviewerRoleDevOpsReviewer,
		ReviewerRoleQAReviewer,
	}
}
