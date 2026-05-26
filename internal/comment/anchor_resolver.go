package comment

import (
	"strings"

	"technical-specification-review-agent/internal/domain"
)

type resolvedAnchor struct {
	line   *int
	quoted string
}

func resolveFindingAnchor(document domain.Document, finding domain.Finding) resolvedAnchor {
	if anchor := resolveAnchorBySectionHeading(document, finding); anchor.line != nil {
		return anchor
	}

	quoted := quoteForComment(finding.SourceChunk)
	return resolvedAnchor{
		line:   findAnchorLine(document.NormalizedContent, finding.SourceChunk),
		quoted: quoted,
	}
}

func resolveAnchorBySectionHeading(document domain.Document, finding domain.Finding) resolvedAnchor {
	for _, block := range document.Blocks {
		if block.Kind != "heading" {
			continue
		}
		if !matchesFindingSection(block.SectionTitle, block.Text, finding) {
			continue
		}
		if line := findLineByExactOrContainedText(document.NormalizedContent, block.Text); line != nil {
			return resolvedAnchor{
				line:   line,
				quoted: safeTruncate(strings.TrimSpace(block.Text), 220),
			}
		}
	}

	for _, section := range document.Sections {
		if !matchesFindingSection(section.Title, section.Title, finding) {
			continue
		}
		if line := findLineByExactOrContainedText(document.NormalizedContent, section.Title); line != nil {
			return resolvedAnchor{
				line:   line,
				quoted: safeTruncate(strings.TrimSpace(section.Title), 220),
			}
		}
	}

	return resolvedAnchor{}
}

func matchesFindingSection(sectionTitle, headingText string, finding domain.Finding) bool {
	targetTitle := normalizeAnchorText(finding.SectionTitle)
	if targetTitle == "" {
		return false
	}

	candidates := []string{
		normalizeAnchorText(sectionTitle),
		normalizeAnchorText(headingText),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if candidate == targetTitle || strings.HasPrefix(candidate, targetTitle+"_") || strings.HasPrefix(targetTitle, candidate+"_") {
			return true
		}
	}
	return false
}

func findLineByExactOrContainedText(documentText, target string) *int {
	documentText = strings.TrimSpace(documentText)
	target = strings.TrimSpace(target)
	if documentText == "" || target == "" {
		return nil
	}

	lines := strings.Split(documentText, "\n")
	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if trimmed == target || strings.Contains(trimmed, target) {
			lineNumber := idx + 1
			return &lineNumber
		}
	}
	return nil
}

func normalizeAnchorText(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\t", " ")
	return strings.Join(strings.Fields(value), "_")
}
