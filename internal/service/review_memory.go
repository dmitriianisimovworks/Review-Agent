package service

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"technical-specification-review-agent/internal/domain"
)

const (
	reviewMemoryRunLimit             = 5
	reviewMemorySummaryLimit         = 3
	reviewMemoryFindingsLimit        = 12
	reviewMemoryArchitecturesLimit   = 4
	reviewMemoryModulesLimit         = 8
	reviewMemoryRolesLimit           = 8
	reviewMemoryEntitiesLimit        = 12
	reviewMemoryGlossaryLimit        = 12
	reviewMemoryDecisionsLimit       = 8
	reviewMemoryResolvedLimit        = 10
	reviewMemorySectionsLimit        = 8
	reviewMemorySectionProblemsLimit = 4
)

func buildReviewMemory(reviewKey string, analyses []domain.Analysis) domain.ReviewMemory {
	memory := domain.ReviewMemory{
		ReviewKey:     reviewKey,
		PriorRunCount: len(analyses),
	}
	if reviewKey == "" || len(analyses) == 0 {
		return memory
	}

	summaries := make([]string, 0, reviewMemorySummaryLimit)
	seenSummaries := map[string]struct{}{}
	knownFindings := make([]domain.Finding, 0)
	architectureNotes := make([]string, 0, reviewMemoryArchitecturesLimit)
	seenArchitecture := map[string]struct{}{}
	modules := make([]string, 0, reviewMemoryModulesLimit)
	roles := make([]string, 0, reviewMemoryRolesLimit)
	entities := make([]string, 0, reviewMemoryEntitiesLimit)
	glossary := make([]string, 0, reviewMemoryGlossaryLimit)
	decisions := make([]string, 0, reviewMemoryDecisionsLimit)
	seenModules := map[string]struct{}{}
	seenRoles := map[string]struct{}{}
	seenEntities := map[string]struct{}{}
	seenGlossary := map[string]struct{}{}
	seenDecisions := map[string]struct{}{}
	resolved := make([]domain.FindingRef, 0, reviewMemoryResolvedLimit)
	seenResolved := map[string]struct{}{}
	sections := make([]domain.MemorySection, 0, reviewMemorySectionsLimit)
	seenSections := map[string]int{}

	for _, analysis := range analyses {
		summary := strings.TrimSpace(analysis.Summary)
		if summary != "" {
			key := strings.ToLower(summary)
			if _, exists := seenSummaries[key]; !exists {
				seenSummaries[key] = struct{}{}
				summaries = append(summaries, summary)
			}
		}

		for _, finding := range analysis.Findings {
			knownFindings = append(knownFindings, finding)

			if isArchitecturalMemoryCandidate(finding) && len(architectureNotes) < reviewMemoryArchitecturesLimit {
				note := strings.TrimSpace(finding.HowToFix)
				if note == "" {
					note = strings.TrimSpace(finding.Problem)
				}
				key := strings.ToLower(note)
				if note != "" {
					if _, exists := seenArchitecture[key]; !exists {
						seenArchitecture[key] = struct{}{}
						architectureNotes = append(architectureNotes, note)
					}
				}
			}
		}

		appendUniqueStrings(&modules, seenModules, analysis.Memory.Modules, reviewMemoryModulesLimit)
		appendUniqueStrings(&roles, seenRoles, analysis.Memory.UserRoles, reviewMemoryRolesLimit)
		appendUniqueStrings(&entities, seenEntities, analysis.Memory.Entities, reviewMemoryEntitiesLimit)
		appendUniqueStrings(&glossary, seenGlossary, analysis.Memory.Glossary, reviewMemoryGlossaryLimit)
		appendUniqueStrings(&decisions, seenDecisions, analysis.Memory.ArchitectureDecisions, reviewMemoryDecisionsLimit)
		appendUniqueFindingRefs(&resolved, seenResolved, analysis.Memory.ResolvedFindings, reviewMemoryResolvedLimit)
		mergeMemorySections(&sections, seenSections, analysis.Memory.Sections, reviewMemorySectionsLimit)
	}

	if len(summaries) > reviewMemorySummaryLimit {
		summaries = summaries[:reviewMemorySummaryLimit]
	}

	memory.PriorSummaries = summarizeStrings(summaries)
	memory.KnownFindings = compactMemoryFindings(knownFindings)
	memory.ArchitecturalNotes = summarizeStrings(architectureNotes)
	memory.Modules = summarizeStrings(modules)
	memory.UserRoles = summarizeStrings(roles)
	memory.Entities = summarizeStrings(entities)
	memory.Glossary = summarizeStrings(glossary)
	memory.ArchitectureDecisions = summarizeStrings(decisions)
	memory.ResolvedFindings = resolved
	memory.Sections = sections
	return memory
}

func enrichReviewMemory(memory domain.ReviewMemory, document domain.Document, chunks []domain.AnalysisChunk, findings []domain.Finding, mode domain.AnalysisMode) domain.ReviewMemory {
	modules := mergeSummaryValues(memory.Modules, extractModules(document.RawContent, document.Sections), reviewMemoryModulesLimit)
	userRoles := mergeSummaryValues(memory.UserRoles, extractUserRoles(document.RawContent, findings), reviewMemoryRolesLimit)
	entities := mergeSummaryValues(memory.Entities, extractEntities(document.RawContent, findings), reviewMemoryEntitiesLimit)
	glossary := mergeSummaryValues(memory.Glossary, extractGlossary(document.RawContent, document.Sections), reviewMemoryGlossaryLimit)
	decisions := mergeSummaryValues(memory.ArchitectureDecisions, extractArchitectureDecisions(findings), reviewMemoryDecisionsLimit)
	architectureNotes := mergeSummaryValues(memory.ArchitecturalNotes, extractArchitectureNotes(findings), reviewMemoryArchitecturesLimit)
	sections := mergeCurrentSections(memory.Sections, buildSectionMemory(chunks, findings), reviewMemorySectionsLimit)
	resolved := memory.ResolvedFindings
	if mode == domain.AnalysisModeIncrementalReview {
		resolved = mergeResolvedFindingRefs(memory.ResolvedFindings, deriveResolvedFindings(memory, chunks, findings), reviewMemoryResolvedLimit)
		sections = applyResolvedProblemsToSections(sections, resolved)
	}

	memory.Modules = summarizeStrings(modules)
	memory.UserRoles = summarizeStrings(userRoles)
	memory.Entities = summarizeStrings(entities)
	memory.Glossary = summarizeStrings(glossary)
	memory.ArchitectureDecisions = summarizeStrings(decisions)
	memory.ArchitecturalNotes = summarizeStrings(architectureNotes)
	memory.Sections = sections
	memory.ResolvedFindings = resolved
	return memory
}

func compactMemoryFindings(findings []domain.Finding) []domain.Finding {
	deduped := deduplicateFindings(findings)
	sort.SliceStable(deduped, func(i, j int) bool {
		left := findingScore(deduped[i])
		right := findingScore(deduped[j])
		if left == right {
			return deduped[i].Problem < deduped[j].Problem
		}
		return left > right
	})
	if len(deduped) > reviewMemoryFindingsLimit {
		deduped = deduped[:reviewMemoryFindingsLimit]
	}
	return deduped
}

func summarizeStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, safeTruncate(value, 220))
	}
	return result
}

func appendUniqueStrings(target *[]string, seen map[string]struct{}, values []string, limit int) {
	if limit <= 0 {
		return
	}

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		*target = append(*target, value)
		if len(*target) >= limit {
			return
		}
	}
}

func appendUniqueFindingRefs(target *[]domain.FindingRef, seen map[string]struct{}, values []domain.FindingRef, limit int) {
	if limit <= 0 {
		return
	}
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value.Role)) + "|" +
			strings.ToLower(strings.TrimSpace(value.Category)) + "|" +
			strings.ToLower(strings.TrimSpace(value.Problem)) + "|" +
			strings.ToLower(strings.TrimSpace(value.SectionID)) + "|" +
			strings.ToLower(strings.TrimSpace(value.SectionTitle))
		if key == "||||" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		*target = append(*target, value)
		if len(*target) >= limit {
			return
		}
	}
}

func mergeSummaryValues(existing, additions []string, limit int) []string {
	merged := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	appendUniqueStrings(&merged, seen, existing, limit)
	appendUniqueStrings(&merged, seen, additions, limit)
	return merged
}

func extractModules(documentText string, sections []domain.DocumentSection) []string {
	result := make([]string, 0, len(sections))
	for _, section := range sections {
		title := normalizeSectionTitle(section.Title)
		if title == "" || strings.EqualFold(title, "document") {
			continue
		}
		result = append(result, title)
	}
	if len(result) > 0 {
		return result
	}

	lines := strings.Split(documentText, "\n")
	for _, line := range lines {
		line = normalizeSectionTitle(line)
		if line == "" || len(line) > 60 {
			continue
		}
		if strings.Contains(line, ". ") || strings.Contains(line, ":") {
			result = append(result, line)
		}
	}
	return result
}

func extractUserRoles(documentText string, findings []domain.Finding) []string {
	type roleMarker struct {
		pattern string
		label   string
	}

	markers := []roleMarker{
		{pattern: "администратор", label: "Administrator"},
		{pattern: "admin", label: "Administrator"},
		{pattern: "пользователь", label: "User"},
		{pattern: "user", label: "User"},
		{pattern: "менеджер", label: "Manager"},
		{pattern: "manager", label: "Manager"},
		{pattern: "оператор", label: "Operator"},
		{pattern: "operator", label: "Operator"},
		{pattern: "клиент", label: "Client"},
		{pattern: "client", label: "Client"},
		{pattern: "аналитик", label: "Analyst"},
		{pattern: "analyst", label: "Analyst"},
		{pattern: "модератор", label: "Moderator"},
		{pattern: "moderator", label: "Moderator"},
		{pattern: "поддержк", label: "Support"},
		{pattern: "support", label: "Support"},
	}

	haystack := strings.ToLower(documentText + "\n" + findingsText(findings))
	result := make([]string, 0, reviewMemoryRolesLimit)
	seen := make(map[string]struct{}, len(markers))
	for _, marker := range markers {
		if !strings.Contains(haystack, marker.pattern) {
			continue
		}
		if _, exists := seen[marker.label]; exists {
			continue
		}
		seen[marker.label] = struct{}{}
		result = append(result, marker.label)
		if len(result) >= reviewMemoryRolesLimit {
			break
		}
	}
	return result
}

func extractEntities(documentText string, findings []domain.Finding) []string {
	candidates := append(extractDelimitedTerms(documentText), extractDelimitedTerms(findingsText(findings))...)
	result := make([]string, 0, reviewMemoryEntitiesLimit)
	seen := make(map[string]struct{}, len(candidates))
	appendUniqueStrings(&result, seen, candidates, reviewMemoryEntitiesLimit)
	return result
}

func extractGlossary(documentText string, sections []domain.DocumentSection) []string {
	result := make([]string, 0, reviewMemoryGlossaryLimit)
	seen := make(map[string]struct{}, reviewMemoryGlossaryLimit)

	for _, section := range sections {
		title := normalizeSectionTitle(section.Title)
		if title != "" {
			appendUniqueStrings(&result, seen, []string{title}, reviewMemoryGlossaryLimit)
		}
	}

	appendUniqueStrings(&result, seen, extractDefinitionTerms(documentText), reviewMemoryGlossaryLimit)
	appendUniqueStrings(&result, seen, extractDelimitedTerms(documentText), reviewMemoryGlossaryLimit)
	return result
}

func extractArchitectureDecisions(findings []domain.Finding) []string {
	result := make([]string, 0, reviewMemoryDecisionsLimit)
	seen := make(map[string]struct{}, reviewMemoryDecisionsLimit)
	for _, finding := range findings {
		if !isArchitecturalMemoryCandidate(finding) {
			continue
		}
		value := strings.TrimSpace(finding.HowToFix)
		if value == "" {
			continue
		}
		appendUniqueStrings(&result, seen, []string{value}, reviewMemoryDecisionsLimit)
	}
	return result
}

func extractArchitectureNotes(findings []domain.Finding) []string {
	result := make([]string, 0, reviewMemoryArchitecturesLimit)
	seen := make(map[string]struct{}, reviewMemoryArchitecturesLimit)
	for _, finding := range findings {
		if !isArchitecturalMemoryCandidate(finding) {
			continue
		}
		value := strings.TrimSpace(finding.HowToFix)
		if value == "" {
			value = strings.TrimSpace(finding.Problem)
		}
		appendUniqueStrings(&result, seen, []string{value}, reviewMemoryArchitecturesLimit)
	}
	return result
}

func findingsText(findings []domain.Finding) string {
	parts := make([]string, 0, len(findings)*2)
	for _, finding := range findings {
		if finding.Problem != "" {
			parts = append(parts, finding.Problem)
		}
		if finding.HowToFix != "" {
			parts = append(parts, finding.HowToFix)
		}
	}
	return strings.Join(parts, "\n")
}

var (
	sectionPrefixPattern  = regexp.MustCompile(`^\s*\d+(\.\d+)*[\)\.\-]?\s*`)
	backtickPattern       = regexp.MustCompile("`([^`]{2,80})`")
	doubleQuotePattern    = regexp.MustCompile(`"([^"\n]{2,80})"`)
	angleQuotePattern     = regexp.MustCompile(`«([^»\n]{2,80})»`)
	definitionTermPattern = regexp.MustCompile(`(?m)^([A-Za-zА-Яа-я0-9_/\- ]{2,60})\s*[:\-]\s+`)
)

func normalizeSectionTitle(title string) string {
	title = strings.TrimSpace(title)
	title = sectionPrefixPattern.ReplaceAllString(title, "")
	return strings.TrimSpace(title)
}

func extractDelimitedTerms(text string) []string {
	result := make([]string, 0)
	for _, pattern := range []*regexp.Regexp{backtickPattern, doubleQuotePattern, angleQuotePattern} {
		matches := pattern.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			value := normalizeKnowledgeValue(match[1])
			if value == "" {
				continue
			}
			result = append(result, value)
		}
	}
	return result
}

func extractDefinitionTerms(text string) []string {
	matches := definitionTermPattern.FindAllStringSubmatch(text, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		value := normalizeKnowledgeValue(match[1])
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}

func normalizeKnowledgeValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Count(value, " ") > 4 {
		return ""
	}
	return safeTruncate(value, 80)
}

func buildSectionMemory(chunks []domain.AnalysisChunk, findings []domain.Finding) []domain.MemorySection {
	if len(chunks) == 0 {
		return nil
	}

	problemsBySection := make(map[string][]string)
	titlesBySection := make(map[string]string)
	order := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		sectionKey := sectionMemoryKey(chunk.SectionID, chunk.SectionTitle, chunk.ChunkIndex)
		if _, exists := titlesBySection[sectionKey]; !exists {
			order = append(order, sectionKey)
			titlesBySection[sectionKey] = fallbackSectionTitle(chunk.SectionTitle, chunk.ChunkIndex)
		}
	}

	for _, finding := range findings {
		if finding.ChunkIndex < 0 || finding.ChunkIndex >= len(chunks) {
			continue
		}
		chunk := chunks[finding.ChunkIndex]
		sectionKey := sectionMemoryKey(chunk.SectionID, chunk.SectionTitle, chunk.ChunkIndex)
		problem := strings.TrimSpace(finding.Problem)
		if problem == "" {
			continue
		}
		problemsBySection[sectionKey] = appendUniqueProblem(problemsBySection[sectionKey], problem, reviewMemorySectionProblemsLimit)
	}

	result := make([]domain.MemorySection, 0, minInt(len(order), reviewMemorySectionsLimit))
	for _, sectionKey := range order {
		result = append(result, domain.MemorySection{
			SectionID:     extractSectionID(sectionKey),
			SectionTitle:  titlesBySection[sectionKey],
			KnownProblems: problemsBySection[sectionKey],
		})
		if len(result) >= reviewMemorySectionsLimit {
			break
		}
	}
	return result
}

func mergeMemorySections(target *[]domain.MemorySection, seen map[string]int, values []domain.MemorySection, limit int) {
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value.SectionID)) + "|" + strings.ToLower(strings.TrimSpace(value.SectionTitle))
		if index, exists := seen[key]; exists {
			(*target)[index].KnownProblems = mergeSummaryValues((*target)[index].KnownProblems, value.KnownProblems, reviewMemorySectionProblemsLimit)
			(*target)[index].ResolvedProblems = mergeSummaryValues((*target)[index].ResolvedProblems, value.ResolvedProblems, reviewMemorySectionProblemsLimit)
			continue
		}
		if len(*target) >= limit {
			return
		}
		seen[key] = len(*target)
		*target = append(*target, value)
	}
}

func mergeCurrentSections(existing, current []domain.MemorySection, limit int) []domain.MemorySection {
	merged := make([]domain.MemorySection, 0, limit)
	seen := make(map[string]int, limit)
	mergeMemorySections(&merged, seen, current, limit)
	mergeMemorySections(&merged, seen, existing, limit)
	return merged
}

func deriveResolvedFindings(memory domain.ReviewMemory, chunks []domain.AnalysisChunk, current []domain.Finding) []domain.FindingRef {
	if len(memory.Sections) == 0 || len(chunks) == 0 {
		return nil
	}

	currentProblemsBySection := make(map[string]map[string]struct{}, len(chunks))
	sectionMeta := make(map[string]domain.AnalysisChunk, len(chunks))
	for _, chunk := range chunks {
		key := sectionMemoryKey(chunk.SectionID, chunk.SectionTitle, chunk.ChunkIndex)
		if _, exists := currentProblemsBySection[key]; !exists {
			currentProblemsBySection[key] = make(map[string]struct{})
			sectionMeta[key] = chunk
		}
	}
	for _, finding := range current {
		if finding.ChunkIndex < 0 || finding.ChunkIndex >= len(chunks) {
			continue
		}
		chunk := chunks[finding.ChunkIndex]
		key := sectionMemoryKey(chunk.SectionID, chunk.SectionTitle, chunk.ChunkIndex)
		currentProblemsBySection[key][strings.ToLower(strings.TrimSpace(finding.Problem))] = struct{}{}
	}

	result := make([]domain.FindingRef, 0, reviewMemoryResolvedLimit)
	seen := make(map[string]struct{}, reviewMemoryResolvedLimit)
	for _, section := range memory.Sections {
		key := sectionMemoryKey(section.SectionID, section.SectionTitle, 0)
		currentProblems, reviewed := currentProblemsBySection[key]
		if !reviewed {
			continue
		}
		meta := sectionMeta[key]
		for _, problem := range section.KnownProblems {
			if _, stillExists := currentProblems[strings.ToLower(strings.TrimSpace(problem))]; stillExists {
				continue
			}
			ref := domain.FindingRef{
				Problem:      problem,
				SectionID:    meta.SectionID,
				SectionTitle: fallbackSectionTitle(meta.SectionTitle, meta.ChunkIndex),
			}
			appendUniqueFindingRefs(&result, seen, []domain.FindingRef{ref}, reviewMemoryResolvedLimit)
		}
	}
	return result
}

func sectionMemoryKey(sectionID, sectionTitle string, chunkIndex int) string {
	if strings.TrimSpace(sectionID) != "" {
		return strings.TrimSpace(sectionID) + "|" + strings.TrimSpace(sectionTitle)
	}
	if strings.TrimSpace(sectionTitle) != "" {
		return strings.TrimSpace(sectionTitle) + "|" + strings.TrimSpace(sectionTitle)
	}
	return "chunk|" + fallbackSectionTitle("", chunkIndex)
}

func extractSectionID(key string) string {
	parts := strings.SplitN(key, "|", 2)
	if len(parts) == 0 {
		return ""
	}
	if parts[0] == "chunk" {
		return ""
	}
	return parts[0]
}

func fallbackSectionTitle(title string, chunkIndex int) string {
	title = strings.TrimSpace(title)
	if title != "" {
		return title
	}
	return "Chunk " + strconv.Itoa(chunkIndex+1)
}

func appendUniqueProblem(values []string, problem string, limit int) []string {
	problem = safeTruncate(strings.TrimSpace(problem), 160)
	if problem == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(existing, problem) {
			return values
		}
	}
	if len(values) >= limit {
		return values
	}
	return append(values, problem)
}

func mergeResolvedFindingRefs(existing, current []domain.FindingRef, limit int) []domain.FindingRef {
	merged := make([]domain.FindingRef, 0, limit)
	seen := make(map[string]struct{}, limit)
	appendUniqueFindingRefs(&merged, seen, current, limit)
	appendUniqueFindingRefs(&merged, seen, existing, limit)
	return merged
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func applyResolvedProblemsToSections(sections []domain.MemorySection, resolved []domain.FindingRef) []domain.MemorySection {
	indexByKey := make(map[string]int, len(sections))
	for idx, section := range sections {
		key := strings.ToLower(strings.TrimSpace(section.SectionID)) + "|" + strings.ToLower(strings.TrimSpace(section.SectionTitle))
		indexByKey[key] = idx
	}

	for _, finding := range resolved {
		key := strings.ToLower(strings.TrimSpace(finding.SectionID)) + "|" + strings.ToLower(strings.TrimSpace(finding.SectionTitle))
		index, exists := indexByKey[key]
		if !exists {
			continue
		}
		sections[index].ResolvedProblems = appendUniqueProblem(sections[index].ResolvedProblems, finding.Problem, reviewMemorySectionProblemsLimit)
	}

	return sections
}

func safeTruncate(value string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(value))
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return string(runes)
	}

	cut := maxRunes
	for cut > 0 && runes[cut-1] != ' ' && runes[cut-1] != '\n' && runes[cut-1] != '\t' {
		cut--
	}
	if cut == 0 {
		cut = maxRunes
	}
	return strings.TrimSpace(string(runes[:cut])) + "..."
}

func isArchitecturalMemoryCandidate(finding domain.Finding) bool {
	switch finding.Role {
	case string(domain.ReviewerRoleTechLead),
		string(domain.ReviewerRoleSolutionArchitect),
		string(domain.ReviewerRoleSeniorBackendEngineer),
		string(domain.ReviewerRoleDevOpsReviewer):
	default:
		return false
	}

	return finding.Severity == domain.SeverityCritical ||
		finding.Severity == domain.SeverityError ||
		finding.Category == "contradiction" ||
		finding.Category == "technical_risk" ||
		finding.Category == "scalability_risk" ||
		finding.Category == "devops_risk" ||
		finding.Category == "api_problem"
}
