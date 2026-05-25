package domain

type ArchitectureDecision struct {
	Decision string `json:"decision"`
	Context  string `json:"context,omitempty"`
	Rationale string `json:"rationale,omitempty"`
	Status   string `json:"status,omitempty"`
}

type ReviewMemory struct {
	ReviewKey          string
	PriorRunCount      int
	PriorSummaries     []string
	KnownFindings      []Finding
	ArchitecturalNotes []string
	ArchitectureDecisions []ArchitectureDecision
	Glossary           []string
	Entities           []string
	Modules            []string
	UserRoles          []string
}

func (m ReviewMemory) HasContext() bool {
	return m.ReviewKey != "" && (m.PriorRunCount > 0 ||
		len(m.KnownFindings) > 0 ||
		len(m.PriorSummaries) > 0 ||
		len(m.ArchitecturalNotes) > 0 ||
		len(m.ArchitectureDecisions) > 0 ||
		len(m.Glossary) > 0 ||
		len(m.Entities) > 0 ||
		len(m.Modules) > 0 ||
		len(m.UserRoles) > 0)
}
