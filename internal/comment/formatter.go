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

func (f *DefaultFormatter) Format(document domain.Document, analysis domain.Analysis, mode PublishMode) []Draft {
	switch mode {
	case PublishModeInline:
		return buildInlineDrafts(document, analysis)
	case PublishModeSummary:
		return buildSummaryDrafts(analysis)
	default:
		drafts := buildInlineDrafts(document, analysis)
		return append(drafts, buildSummaryDrafts(analysis)...)
	}
}

func buildInlineDrafts(document domain.Document, analysis domain.Analysis) []Draft {
	drafts := make([]Draft, 0, len(analysis.Findings))
	for _, finding := range analysis.Findings {
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

func buildSummaryDrafts(analysis domain.Analysis) []Draft {
	if len(analysis.Findings) == 0 {
		return []Draft{{
			Type:    "summary",
			Content: "Итоговый комментарий\n\nСущественных замечаний по документу не обнаружено.",
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

	limit := min(len(findings), 5)
	lines := []string{
		"Итоговый комментарий",
		"",
		analysis.Summary,
		"",
		"Ключевые замечания:",
	}
	for i := 0; i < limit; i++ {
		lines = append(lines, fmt.Sprintf("%d. [%s] %s", i+1, findings[i].Severity, strings.TrimSpace(findings[i].Problem)))
	}

	return []Draft{{
		Type:    "summary",
		Content: strings.Join(lines, "\n"),
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
