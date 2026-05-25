package domain

type ReviewMemory struct {
	ReviewKey          string
	PriorRunCount      int
	PriorSummaries     []string
	KnownFindings      []Finding
	ArchitecturalNotes []string
}

func (m ReviewMemory) HasContext() bool {
	return m.ReviewKey != "" && (m.PriorRunCount > 0 || len(m.KnownFindings) > 0 || len(m.PriorSummaries) > 0 || len(m.ArchitecturalNotes) > 0)
}
