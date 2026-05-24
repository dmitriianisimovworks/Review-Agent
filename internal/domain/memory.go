package domain

type ReviewMemory struct {
	ReviewKey             string
	PriorRunCount         int
	PriorSummaries        []string
	KnownFindings         []Finding
	ResolvedFindings      []FindingRef
	ArchitecturalNotes    []string
	Modules               []string
	UserRoles             []string
	Entities              []string
	Glossary              []string
	ArchitectureDecisions []string
	Sections              []MemorySection
}

type FindingRef struct {
	Role         string `json:"role"`
	Category     string `json:"category"`
	Severity     string `json:"severity"`
	Problem      string `json:"problem"`
	SectionID    string `json:"section_id,omitempty"`
	SectionTitle string `json:"section_title,omitempty"`
}

type MemorySection struct {
	SectionID        string   `json:"section_id,omitempty"`
	SectionTitle     string   `json:"section_title"`
	KnownProblems    []string `json:"known_problems,omitempty"`
	ResolvedProblems []string `json:"resolved_problems,omitempty"`
}

func (m ReviewMemory) HasContext() bool {
	return m.ReviewKey != "" && (m.PriorRunCount > 0 ||
		len(m.KnownFindings) > 0 ||
		len(m.ResolvedFindings) > 0 ||
		len(m.PriorSummaries) > 0 ||
		len(m.ArchitecturalNotes) > 0 ||
		len(m.Modules) > 0 ||
		len(m.UserRoles) > 0 ||
		len(m.Entities) > 0 ||
		len(m.Glossary) > 0 ||
		len(m.ArchitectureDecisions) > 0 ||
		len(m.Sections) > 0)
}
