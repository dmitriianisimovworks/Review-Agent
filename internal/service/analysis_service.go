package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"technical-specification-review-agent/internal/apperrors"
	"technical-specification-review-agent/internal/config"
	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/integration/google"
	"technical-specification-review-agent/internal/integration/llm"
	"technical-specification-review-agent/internal/parser"
	"technical-specification-review-agent/internal/repository"
	"technical-specification-review-agent/internal/reviewconfig"
)

type AnalysisService struct {
	documentRepo     repository.DocumentRepository
	analysisRepo     repository.AnalysisRepository
	analysisCache    repository.AnalysisCache
	llmClient        llm.Client
	documentReader   google.DocumentReader
	commentPublisher google.CommentPublisher
	llmProvider      string
	llmModel         string
	documentConfig   config.DocumentConfig
	reviewConfig     reviewconfig.Provider
}

const maxFindingsPerRole = 5
const maxContradictionPairs = 8

type StartAnalysisInput struct {
	Name         string
	Content      string
	GoogleDocURL string
	ContextKey   string
	Source       domain.DocumentSource
	Mode         domain.AnalysisMode
}

func NewAnalysisService(
	documentRepo repository.DocumentRepository,
	analysisRepo repository.AnalysisRepository,
	analysisCache repository.AnalysisCache,
	llmClient llm.Client,
	documentReader google.DocumentReader,
	commentPublisher google.CommentPublisher,
	llmProvider string,
	llmModel string,
	documentConfig config.DocumentConfig,
	reviewConfig reviewconfig.Provider,
) *AnalysisService {
	return &AnalysisService{
		documentRepo:     documentRepo,
		analysisRepo:     analysisRepo,
		analysisCache:    analysisCache,
		llmClient:        llmClient,
		documentReader:   documentReader,
		commentPublisher: commentPublisher,
		llmProvider:      llmProvider,
		llmModel:         llmModel,
		documentConfig:   documentConfig,
		reviewConfig:     reviewConfig,
	}
}

func (s *AnalysisService) StartAnalysis(ctx context.Context, input StartAnalysisInput) (domain.Analysis, error) {
	now := time.Now().UTC()
	source := input.Source
	if source == "" {
		source = domain.DocumentSourceUpload
	}
	mode := input.Mode
	if mode == "" {
		mode = domain.AnalysisModeFullReview
	}
	settings, err := s.loadReviewConfig()
	if err != nil {
		return domain.Analysis{}, err
	}

	content := strings.TrimSpace(input.Content)
	documentName := strings.TrimSpace(input.Name)
	externalID := ""
	reviewKey := strings.TrimSpace(input.ContextKey)
	var structuredDoc google.Document

	if source == domain.DocumentSourceGoogleDocs {
		if s.documentReader == nil {
			return domain.Analysis{}, apperrors.New(apperrors.KindInternal, "google docs reader is not configured")
		}
		if strings.TrimSpace(input.GoogleDocURL) == "" {
			return domain.Analysis{}, apperrors.New(apperrors.KindInvalidArgument, "google_doc_url is required for google_docs source")
		}

		doc, err := s.documentReader.Read(ctx, input.GoogleDocURL)
		if err != nil {
			return domain.Analysis{}, apperrors.Wrap(apperrors.KindDependency, "failed to read google document", err)
		}
		structuredDoc = doc
		content = doc.Content
		externalID = doc.ExternalID
		if documentName == "" {
			documentName = doc.Title
		}
	}

	if content == "" {
		return domain.Analysis{}, apperrors.New(apperrors.KindInvalidArgument, "document content is required")
	}

	reviewKey = deriveReviewKey(source, reviewKey, externalID, documentName)
	var previousAnalyses []domain.Analysis
	if settings.MemoryEnabled {
		previousAnalyses, err = s.analysisRepo.ListByReviewKey(ctx, reviewKey, reviewMemoryRunLimit)
		if err != nil {
			return domain.Analysis{}, apperrors.Wrap(apperrors.KindInternal, "failed to load review history", err)
		}
	}
	promptMemory := buildReviewMemory(reviewKey, previousAnalyses)
	if !settings.MemoryEnabled {
		promptMemory = domain.ReviewMemory{}
	}

	document := domain.Document{
		ID:         fmt.Sprintf("doc_%d", now.UnixNano()),
		Name:       documentName,
		Source:     source,
		ExternalID: externalID,
		ReviewKey:  reviewKey,
		RawContent: content,
		Sections:   toDomainSections(structuredDoc.Sections),
		Blocks:     toDomainBlocks(structuredDoc.Blocks),
		CreatedAt:  now,
	}

	documentParser := parser.NewChunkingParser(config.DocumentConfig{
		ChunkSize: settings.ChunkSize,
		MaxChunks: settings.MaxChunks,
	})
	parsed, err := documentParser.Parse(ctx, parser.ParseInput{
		Content:  document.RawContent,
		Sections: document.Sections,
	})
	if err != nil {
		return domain.Analysis{}, apperrors.Wrap(apperrors.KindInternal, "failed to parse document", err)
	}
	document.NormalizedContent = parsed.Text

	if err := s.documentRepo.Save(ctx, document); err != nil {
		return domain.Analysis{}, apperrors.Wrap(apperrors.KindInternal, "failed to store document", err)
	}

	aggregatedFindings := make([]domain.Finding, 0)
	chunks, findings, suppressedFindings, err := s.analyzeChunksByRoles(ctx, document, parsed, mode, now, promptMemory, settings.Roles)
	if err != nil {
		return domain.Analysis{}, err
	}
	aggregatedFindings = findings
	contradictionFindings, err := s.analyzeCrossSectionContradictions(ctx, document, parsed, mode, promptMemory)
	if err != nil {
		return domain.Analysis{}, err
	}
	aggregatedFindings = append(aggregatedFindings, contradictionFindings...)
	aggregatedFindings = deduplicateFindings(aggregatedFindings)
	analysisMemory := promptMemory
	if settings.MemoryEnabled {
		analysisMemory = enrichReviewMemory(promptMemory, document, chunks, aggregatedFindings, mode)
	}

	completedAt := time.Now().UTC()
	mergeBlocked, blockingFindings := evaluateMergePolicy(aggregatedFindings, settings)
	analysis := domain.Analysis{
		ID:                 fmt.Sprintf("analysis_%d", completedAt.UnixNano()),
		DocumentID:         document.ID,
		Mode:               mode,
		Status:             domain.AnalysisStatusCompleted,
		Provider:           s.llmProvider,
		Model:              s.llmModel,
		ChunkCount:         len(parsed.Chunks),
		MergeBlocked:       mergeBlocked,
		BlockingFindings:   blockingFindings,
		SuppressedFindings: suppressedFindings,
		Findings:           aggregatedFindings,
		Chunks:             chunks,
		Summary:            buildSummary(aggregatedFindings, len(parsed.Chunks)),
		Memory:             analysisMemory,
		DocumentSections:   parsed.Sections,
		CreatedAt:          now,
		CompletedAt:        &completedAt,
	}
	for i := range analysis.Chunks {
		analysis.Chunks[i].AnalysisID = analysis.ID
	}

	if err := s.analysisRepo.Save(ctx, analysis); err != nil {
		return domain.Analysis{}, apperrors.Wrap(apperrors.KindInternal, "failed to store analysis", err)
	}
	if s.analysisCache != nil {
		_ = s.analysisCache.Set(ctx, analysis)
	}

	return analysis, nil
}

func (s *AnalysisService) analyzeChunksByRoles(
	ctx context.Context,
	document domain.Document,
	parsed parser.ParsedDocument,
	mode domain.AnalysisMode,
	now time.Time,
	memory domain.ReviewMemory,
	roles []domain.ReviewerRole,
) ([]domain.AnalysisChunk, []domain.Finding, int, error) {
	type llmOutcome struct {
		chunk    domain.AnalysisChunk
		findings []domain.Finding
	}

	outcomes := make([]llmOutcome, 0, len(parsed.Chunks)*len(roles))
	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(len(roles))

	results := make(chan llmOutcome, len(parsed.Chunks)*len(roles))

	for idx, chunkText := range parsed.Chunks {
		for _, role := range roles {
			idx := idx
			chunkText := chunkText
			role := role
			descriptor := parsedChunkDescriptor(parsed, idx)

			g.Go(func() error {
				result, err := s.llmClient.AnalyzeChunk(groupCtx, llm.AnalyzeInput{
					DocumentName: document.Name,
					DocumentText: parsed.Text,
					ChunkText:    chunkText,
					ChunkIndex:   idx,
					ChunkCount:   len(parsed.Chunks),
					SectionTitle: descriptor.SectionTitle,
					SectionLevel: descriptor.SectionLevel,
					Mode:         mode,
					Source:       document.Source,
					Role:         role,
					Memory:       memory,
				})
				if err != nil {
					return apperrors.Wrap(apperrors.KindDependency, fmt.Sprintf("failed to analyze chunk %d for role %s", idx+1, role), err)
				}
				annotateChunkFindings(result.Findings, descriptor, idx)

				results <- llmOutcome{
					chunk: domain.AnalysisChunk{
						ID:             fmt.Sprintf("chunk_%d_%d_%s", now.UnixNano(), idx, role),
						Role:           role,
						ChunkIndex:     idx,
						ChunkText:      chunkText,
						PromptVersion:  result.PromptVersion,
						SystemPrompt:   result.SystemPrompt,
						UserPrompt:     result.UserPrompt,
						RawLLMResponse: result.RawResponse,
						SectionID:      descriptor.SectionID,
						SectionTitle:   descriptor.SectionTitle,
						SectionLevel:   descriptor.SectionLevel,
						Range:          descriptor.Range,
						CreatedAt:      time.Now().UTC(),
					},
					findings: result.Findings,
				}
				return nil
			})
		}
	}

	if err := g.Wait(); err != nil {
		close(results)
		return nil, nil, 0, err
	}
	close(results)

	for outcome := range results {
		outcomes = append(outcomes, outcome)
	}

	sort.SliceStable(outcomes, func(i, j int) bool {
		if outcomes[i].chunk.ChunkIndex == outcomes[j].chunk.ChunkIndex {
			return outcomes[i].chunk.Role < outcomes[j].chunk.Role
		}
		return outcomes[i].chunk.ChunkIndex < outcomes[j].chunk.ChunkIndex
	})

	chunks := make([]domain.AnalysisChunk, 0, len(outcomes))
	findings := make([]domain.Finding, 0)
	for _, outcome := range outcomes {
		chunks = append(chunks, outcome.chunk)
		findings = append(findings, outcome.findings...)
	}

	filtered, suppressedCount := filterFindingsByMode(filterRoleFindings(findings), memory, mode)
	return chunks, filtered, suppressedCount, nil
}

func (s *AnalysisService) analyzeCrossSectionContradictions(
	ctx context.Context,
	document domain.Document,
	parsed parser.ParsedDocument,
	mode domain.AnalysisMode,
	memory domain.ReviewMemory,
) ([]domain.Finding, error) {
	sections := parsed.Sections
	if len(sections) < 2 {
		return nil, nil
	}

	pairs := buildContradictionPairs(sections, maxContradictionPairs)
	if len(pairs) == 0 {
		return nil, nil
	}

	results := make([]domain.Finding, 0, len(pairs)*2)
	for _, pair := range pairs {
		outcome, err := s.llmClient.AnalyzeSectionPair(ctx, llm.AnalyzeSectionPairInput{
			DocumentName: document.Name,
			Source:       document.Source,
			Mode:         mode,
			SectionA:     pair.left,
			SectionB:     pair.right,
			Memory:       memory,
		})
		if err != nil {
			return nil, apperrors.Wrap(apperrors.KindDependency, fmt.Sprintf("failed cross-section contradiction analysis for %s vs %s", pair.left.Title, pair.right.Title), err)
		}
		for _, finding := range outcome.Findings {
			finding.Category = "contradiction"
			if finding.Role == "" {
				finding.Role = string(domain.ReviewerRoleSolutionArchitect)
			}
			if finding.SectionID == "" {
				finding.SectionID = pair.left.ID
				finding.SectionTitle = pair.left.Title
			}
			if finding.RelatedSectionID == "" {
				finding.RelatedSectionID = pair.right.ID
				finding.RelatedSectionTitle = pair.right.Title
			}
			results = append(results, finding)
		}
	}

	return results, nil
}

type contradictionPair struct {
	left  domain.DocumentSection
	right domain.DocumentSection
	score int
}

func buildContradictionPairs(sections []domain.DocumentSection, limit int) []contradictionPair {
	if len(sections) < 2 || limit <= 0 {
		return nil
	}

	pairs := make([]contradictionPair, 0, len(sections))
	for i := 0; i < len(sections); i++ {
		for j := i + 1; j < len(sections); j++ {
			score := contradictionPairScore(sections[i], sections[j], i, j)
			pairs = append(pairs, contradictionPair{
				left:  sections[i],
				right: sections[j],
				score: score,
			})
		}
	}

	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].score == pairs[j].score {
			if pairs[i].left.Title == pairs[j].left.Title {
				return pairs[i].right.Title < pairs[j].right.Title
			}
			return pairs[i].left.Title < pairs[j].left.Title
		}
		return pairs[i].score > pairs[j].score
	})

	if len(pairs) > limit {
		pairs = pairs[:limit]
	}
	return pairs
}

func contradictionPairScore(left, right domain.DocumentSection, leftIndex, rightIndex int) int {
	score := 0
	leftTokens := titleTokens(left.Title)
	rightTokens := titleTokens(right.Title)
	rightSet := make(map[string]struct{}, len(rightTokens))
	for _, token := range rightTokens {
		rightSet[token] = struct{}{}
	}
	for _, token := range leftTokens {
		if _, exists := rightSet[token]; exists {
			score += 5
		}
	}
	if left.Level > 0 && left.Level == right.Level {
		score += 2
	}
	if distance := rightIndex - leftIndex; distance > 0 && distance <= 2 {
		score += 3
	}
	if mayContainContradictionSignals(left.Content) || mayContainContradictionSignals(right.Content) {
		score += 4
	}
	return score
}

func titleTokens(value string) []string {
	value = strings.ToLower(normalizeKeyPart(value))
	if value == "" {
		return nil
	}
	raw := strings.Split(value, "_")
	result := make([]string, 0, len(raw))
	for _, token := range raw {
		if len(token) < 4 {
			continue
		}
		result = append(result, token)
	}
	return result
}

func mayContainContradictionSignals(value string) bool {
	lowered := strings.ToLower(value)
	signals := []string{"должен", "не должен", "только", "всегда", "никогда", "запрещ", "разреш", "обяз", "может"}
	for _, signal := range signals {
		if strings.Contains(lowered, signal) {
			return true
		}
	}
	return false
}

func annotateChunkFindings(findings []domain.Finding, descriptor parser.ParsedChunk, chunkIndex int) {
	for idx := range findings {
		findings[idx].ChunkIndex = chunkIndex
		findings[idx].SectionID = descriptor.SectionID
		findings[idx].SectionTitle = descriptor.SectionTitle
	}
}

func (s *AnalysisService) GetAnalysis(ctx context.Context, id string) (domain.Analysis, error) {
	if s.analysisCache != nil {
		analysis, found, err := s.analysisCache.Get(ctx, id)
		if err == nil && found {
			return analysis, nil
		}
		if err != nil {
			_ = s.analysisCache.Delete(ctx, id)
		}
	}

	analysis, err := s.analysisRepo.GetByID(ctx, id)
	if err != nil {
		return domain.Analysis{}, apperrors.Wrap(apperrors.KindNotFound, "analysis not found", err)
	}
	if s.analysisCache != nil {
		_ = s.analysisCache.Set(ctx, analysis)
	}
	return analysis, nil
}

func buildSummary(findings []domain.Finding, chunkCount int) string {
	if len(findings) == 0 {
		return fmt.Sprintf("Анализ завершён. Обработано фрагментов: %d. Существенных проблем не обнаружено.", chunkCount)
	}

	severityCounts := map[domain.Severity]int{}
	categoryCounts := map[string]int{}
	for _, finding := range findings {
		severityCounts[finding.Severity]++
		categoryCounts[finding.Category]++
	}

	topCategories := make([]string, 0, len(categoryCounts))
	for category := range categoryCounts {
		topCategories = append(topCategories, category)
	}

	sort.Slice(topCategories, func(i, j int) bool {
		if categoryCounts[topCategories[i]] == categoryCounts[topCategories[j]] {
			return topCategories[i] < topCategories[j]
		}
		return categoryCounts[topCategories[i]] > categoryCounts[topCategories[j]]
	})

	if len(topCategories) > 3 {
		topCategories = topCategories[:3]
	}

	return fmt.Sprintf(
		"Анализ завершён. Обработано фрагментов: %d. После фильтрации оставлено %d замечаний: %d CRITICAL, %d ERROR, %d WARNING. Основные категории: %s.",
		chunkCount,
		len(findings),
		severityCounts[domain.SeverityCritical],
		severityCounts[domain.SeverityError],
		severityCounts[domain.SeverityWarning],
		strings.Join(topCategories, ", "),
	)
}

func filterRoleFindings(findings []domain.Finding) []domain.Finding {
	grouped := make(map[string][]domain.Finding)
	roleOrder := make([]string, 0)
	for _, finding := range findings {
		if finding.Role == "" {
			finding.Role = string(domain.ReviewerRoleSolutionArchitect)
		}
		if _, exists := grouped[finding.Role]; !exists {
			roleOrder = append(roleOrder, finding.Role)
		}
		grouped[finding.Role] = append(grouped[finding.Role], finding)
	}

	filtered := make([]domain.Finding, 0, len(findings))
	for _, role := range roleOrder {
		roleFindings := deduplicateFindings(grouped[role])
		sort.SliceStable(roleFindings, func(i, j int) bool {
			left := findingScore(roleFindings[i])
			right := findingScore(roleFindings[j])
			if left == right {
				if roleFindings[i].Category == roleFindings[j].Category {
					return roleFindings[i].Problem < roleFindings[j].Problem
				}
				return roleFindings[i].Category < roleFindings[j].Category
			}
			return left > right
		})
		if len(roleFindings) > maxFindingsPerRole {
			roleFindings = roleFindings[:maxFindingsPerRole]
		}
		filtered = append(filtered, roleFindings...)
	}

	return filtered
}

func (s *AnalysisService) loadReviewConfig() (reviewconfig.Settings, error) {
	if s.reviewConfig == nil {
		return reviewconfig.Settings{
			Roles:           domain.DefaultReviewerRoles(),
			InlineComments:  true,
			SummaryComments: true,
			MemoryEnabled:   true,
			ChunkSize:       s.documentConfig.ChunkSize,
			MaxChunks:       s.documentConfig.MaxChunks,
		}, nil
	}

	settings, err := s.reviewConfig.Load()
	if err != nil {
		return reviewconfig.Settings{}, apperrors.Wrap(apperrors.KindInvalidArgument, "failed to load review config", err)
	}
	return settings, nil
}

func evaluateMergePolicy(findings []domain.Finding, settings reviewconfig.Settings) (bool, int) {
	if !settings.CriticalBlockMerge {
		return false, 0
	}

	blockingFindings := 0
	for _, finding := range findings {
		if finding.Severity == domain.SeverityCritical {
			blockingFindings++
		}
	}

	return blockingFindings > 0, blockingFindings
}

func filterFindingsByMode(findings []domain.Finding, memory domain.ReviewMemory, mode domain.AnalysisMode) ([]domain.Finding, int) {
	if mode != domain.AnalysisModeIncrementalReview || !memory.HasContext() || len(memory.KnownFindings) == 0 {
		return findings, 0
	}

	known := make(map[string]domain.Finding, len(memory.KnownFindings))
	for _, finding := range memory.KnownFindings {
		known[memoryFindingKey(finding)] = finding
	}

	filtered := make([]domain.Finding, 0, len(findings))
	suppressedCount := 0
	for _, finding := range findings {
		existing, exists := known[memoryFindingKey(finding)]
		if !exists {
			filtered = append(filtered, finding)
			continue
		}
		if findingScore(finding) > findingScore(existing) {
			filtered = append(filtered, finding)
			continue
		}
		suppressedCount++
	}

	return filtered, suppressedCount
}

func deduplicateFindings(findings []domain.Finding) []domain.Finding {
	result := make([]domain.Finding, 0, len(findings))
	seen := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		key := strings.ToLower(strings.TrimSpace(finding.Role)) + "|" +
			strings.ToLower(strings.TrimSpace(finding.Category)) + "|" +
			strings.ToLower(strings.TrimSpace(finding.Problem))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, finding)
	}
	return result
}

func memoryFindingKey(finding domain.Finding) string {
	return strings.ToLower(strings.TrimSpace(finding.Role)) + "|" +
		strings.ToLower(strings.TrimSpace(finding.Category)) + "|" +
		strings.ToLower(strings.TrimSpace(finding.Problem))
}

func findingScore(finding domain.Finding) int {
	score := 0
	switch finding.Severity {
	case domain.SeverityCritical:
		score += 400
	case domain.SeverityError:
		score += 300
	case domain.SeverityWarning:
		score += 200
	default:
		score += 100
	}

	switch finding.Category {
	case "contradiction":
		score += 40
	case "security_risk":
		score += 35
	case "technical_risk", "scalability_risk", "devops_risk":
		score += 30
	case "missing_requirement":
		score += 20
	case "ambiguity":
		score += 10
	}

	return score
}

func deriveReviewKey(source domain.DocumentSource, contextKey, externalID, name string) string {
	if trimmed := normalizeKeyPart(contextKey); trimmed != "" {
		return trimmed
	}
	if trimmed := normalizeKeyPart(externalID); trimmed != "" {
		return string(source) + ":" + trimmed
	}
	if trimmed := normalizeKeyPart(name); trimmed != "" {
		return string(source) + ":" + trimmed
	}
	return string(source) + ":unnamed_document"
}

func normalizeKeyPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.Join(strings.Fields(value), "_")
	return value
}

func parsedChunkDescriptor(parsed parser.ParsedDocument, idx int) parser.ParsedChunk {
	if idx >= 0 && idx < len(parsed.ChunkDescriptors) {
		return parsed.ChunkDescriptors[idx]
	}
	return parser.ParsedChunk{}
}

func toDomainSections(sections []google.Section) []domain.DocumentSection {
	result := make([]domain.DocumentSection, 0, len(sections))
	for _, section := range sections {
		result = append(result, domain.DocumentSection{
			ID:    section.ID,
			Title: section.Title,
			Level: section.Level,
			Range: domain.DocumentRange{
				StartIndex: section.Range.StartIndex,
				EndIndex:   section.Range.EndIndex,
			},
			Content: section.Content,
		})
	}
	return result
}

func toDomainBlocks(blocks []google.Block) []domain.DocumentBlock {
	result := make([]domain.DocumentBlock, 0, len(blocks))
	for _, block := range blocks {
		result = append(result, domain.DocumentBlock{
			Kind:         block.Kind,
			Text:         block.Text,
			Range:        domain.DocumentRange{StartIndex: block.Range.StartIndex, EndIndex: block.Range.EndIndex},
			HeadingLevel: block.HeadingLevel,
			ListLevel:    block.ListLevel,
			SectionID:    block.SectionID,
			SectionTitle: block.SectionTitle,
		})
	}
	return result
}
