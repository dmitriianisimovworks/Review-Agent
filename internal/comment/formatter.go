package comment

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"technical-specification-review-agent/internal/domain"
)

type PublishMode string

const (
	PublishModeInline  PublishMode = "inline"
	PublishModeSummary PublishMode = "summary"
	PublishModeBoth    PublishMode = "both"
)

type Draft struct {
	Type          string
	Content       string
	AnchorLine    *int
	QuotedContent string
}

type Formatter interface {
	Format(document domain.Document, analysis domain.Analysis, mode PublishMode) []Draft
}

type DefaultFormatter struct{}

func NewDefaultFormatter() *DefaultFormatter {
	return &DefaultFormatter{}
}

const (
	maxInlineWarningComments = 3
	maxTotalInlineComments   = 7
	maxSummaryFindings       = 8
)

func (f *DefaultFormatter) Format(document domain.Document, analysis domain.Analysis, mode PublishMode) []Draft {
	switch mode {
	case PublishModeInline:
		return buildInlineDrafts(document, selectInlineFindings(analysis.Findings))
	case PublishModeSummary:
		return buildSummaryDrafts(document, analysis, selectInlineFindings(analysis.Findings))
	default:
		inlineFindings := selectInlineFindings(analysis.Findings)
		drafts := buildInlineDrafts(document, inlineFindings)
		return append(drafts, buildSummaryDrafts(document, analysis, inlineFindings)...)
	}
}

func buildInlineDrafts(document domain.Document, findings []domain.Finding) []Draft {
	drafts := make([]Draft, 0, len(findings))
	for _, finding := range findings {
		draft := Draft{
			Type:          "inline",
			Content:       formatFinding(finding),
			QuotedContent: quoteForComment(finding.SourceChunk),
		}
		if line := findAnchorLine(document.NormalizedContent, finding.SourceChunk); line != nil {
			draft.AnchorLine = line
		}
		drafts = append(drafts, draft)
	}
	return drafts
}

func buildSummaryDrafts(document domain.Document, analysis domain.Analysis, publishedInline []domain.Finding) []Draft {
	endLine := endAnchorLine(document.NormalizedContent)

	if len(analysis.Findings) == 0 {
		return []Draft{{
			Type:       "summary",
			Content:    "Итоговый комментарий\n\nСущественных замечаний по документу не обнаружено.",
			AnchorLine: &endLine,
		}}
	}

	findings := append([]domain.Finding(nil), analysis.Findings...)
	sort.SliceStable(findings, func(i, j int) bool {
		left := severityRank(findings[i].Severity)
		right := severityRank(findings[j].Severity)
		if left == right {
			return findings[i].Category < findings[j].Category
		}
		return left > right
	})

	inlineKeys := make(map[string]struct{}, len(publishedInline))
	for _, finding := range publishedInline {
		inlineKeys[findingKey(finding)] = struct{}{}
	}

	remaining := make([]domain.Finding, 0, len(findings))
	for _, finding := range findings {
		if _, exists := inlineKeys[findingKey(finding)]; exists {
			continue
		}
		remaining = append(remaining, finding)
	}

	lines := []string{
		"Итоговый комментарий",
		"",
		analysis.Summary,
	}

	if len(remaining) > 0 {
		lines = append(lines, "", "Дополнительные замечания:")
		limit := min(len(remaining), maxSummaryFindings)
		for i := 0; i < limit; i++ {
			lines = append(lines, fmt.Sprintf("%d. [%s] %s", i+1, remaining[i].Severity, strings.TrimSpace(remaining[i].Problem)))
		}
		if len(remaining) > limit {
			lines = append(lines, fmt.Sprintf("... и ещё %d замечаний в полном результате анализа.", len(remaining)-limit))
		}
	}

	return []Draft{{
		Type:          "summary",
		Content:       strings.Join(lines, "\n"),
		AnchorLine:    &endLine,
		QuotedContent: quoteLastMeaningfulLine(document.NormalizedContent),
	}}
}

func formatFinding(finding domain.Finding) string {
	return strings.Join([]string{
		fmt.Sprintf("[%s]", finding.Severity),
		"",
		"Проблема:",
		strings.TrimSpace(finding.Problem),
		"",
		"Почему это плохо:",
		strings.TrimSpace(finding.WhyItIsBad),
		"",
		"Как исправить:",
		strings.TrimSpace(finding.HowToFix),
	}, "\n")
}

func findAnchorLine(documentText, sourceChunk string) *int {
	documentText = strings.TrimSpace(documentText)
	sourceChunk = strings.TrimSpace(sourceChunk)
	if documentText == "" || sourceChunk == "" {
		return nil
	}

	index := strings.Index(documentText, sourceChunk)
	if index == -1 {
		return nil
	}

	line := 1
	for _, char := range documentText[:index] {
		if char == '\n' {
			line++
		}
	}
	return &line
}

func quoteForComment(sourceChunk string) string {
	lines := strings.Split(strings.TrimSpace(sourceChunk), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			if len(trimmed) > 220 {
				return trimmed[:220]
			}
			return trimmed
		}
	}
	return ""
}

func quoteLastMeaningfulLine(documentText string) string {
	lines := strings.Split(strings.TrimSpace(documentText), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if len(trimmed) > 220 {
			return trimmed[:220]
		}
		return trimmed
	}
	return ""
}

func severityRank(severity domain.Severity) int {
	switch severity {
	case domain.SeverityCritical:
		return 4
	case domain.SeverityError:
		return 3
	case domain.SeverityWarning:
		return 2
	default:
		return 1
	}
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func endAnchorLine(documentText string) int {
	text := strings.TrimSpace(documentText)
	if text == "" {
		return 1
	}

	line := 1
	for _, char := range text {
		if char == '\n' {
			line++
		}
	}
	return line
}

func selectInlineFindings(findings []domain.Finding) []domain.Finding {
	scored := append([]domain.Finding(nil), findings...)
	sort.SliceStable(scored, func(i, j int) bool {
		left := severityRank(scored[i].Severity)
		right := severityRank(scored[j].Severity)
		if left == right {
			return scored[i].Category < scored[j].Category
		}
		return left > right
	})

	selected := make([]domain.Finding, 0, min(len(scored), maxTotalInlineComments))
	warningCount := 0
	for _, finding := range scored {
		switch finding.Severity {
		case domain.SeverityCritical, domain.SeverityError:
			selected = append(selected, finding)
		case domain.SeverityWarning:
			if warningCount >= maxInlineWarningComments {
				continue
			}
			selected = append(selected, finding)
			warningCount++
		default:
			continue
		}

		if len(selected) >= maxTotalInlineComments {
			break
		}
	}

	return selected
}

func findingKey(finding domain.Finding) string {
	return strings.Join([]string{
		string(finding.Severity),
		finding.Category,
		strings.TrimSpace(finding.Problem),
	}, "|")
}

func MarshalAnchor(line int) string {
	anchor := map[string]any{
		"region": map[string]any{
			"kind": "drive#commentRegion",
			"line": line,
			"rev":  "head",
		},
	}
	data, _ := json.Marshal(anchor)
	return string(data)
}
