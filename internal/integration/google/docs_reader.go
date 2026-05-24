package google

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"google.golang.org/api/docs/v1"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

type ServiceAccountReader struct {
	docsService  *docs.Service
	driveService *drive.Service
}

func NewServiceAccountReader(ctx context.Context, credentialsFile string) (*ServiceAccountReader, error) {
	creds := option.WithCredentialsFile(credentialsFile)

	docsService, err := docs.NewService(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("create docs service: %w", err)
	}

	driveService, err := drive.NewService(ctx, creds)
	if err != nil {
		return nil, fmt.Errorf("create drive service: %w", err)
	}

	return &ServiceAccountReader{
		docsService:  docsService,
		driveService: driveService,
	}, nil
}

func (r *ServiceAccountReader) Read(ctx context.Context, documentURL string) (Document, error) {
	documentID, err := extractDocumentID(documentURL)
	if err != nil {
		return Document{}, err
	}

	driveFile, err := r.driveService.Files.Get(documentID).
		Fields("id,name,mimeType").
		Context(ctx).
		Do()
	if err != nil {
		return Document{}, fmt.Errorf("load drive metadata: %w", err)
	}
	if driveFile.MimeType != "application/vnd.google-apps.document" {
		return Document{}, fmt.Errorf("unsupported google file type: %s", driveFile.MimeType)
	}

	doc, err := r.docsService.Documents.Get(documentID).Context(ctx).Do()
	if err != nil {
		return Document{}, fmt.Errorf("load docs document: %w", err)
	}

	title := strings.TrimSpace(doc.Title)
	if title == "" {
		title = strings.TrimSpace(driveFile.Name)
	}

	blocks := collectBlocks(doc)
	sections := buildSections(blocks)

	return Document{
		ExternalID: documentID,
		Title:      title,
		Content:    renderBlocksText(blocks),
		Sections:   sections,
		Blocks:     blocks,
	}, nil
}

func extractDocumentID(documentURL string) (string, error) {
	trimmed := strings.TrimSpace(documentURL)
	if trimmed == "" {
		return "", errors.New("google doc url is required")
	}

	if !strings.Contains(trimmed, "://") && !strings.Contains(trimmed, "/") {
		return trimmed, nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse google doc url: %w", err)
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := 0; i < len(segments)-1; i++ {
		if segments[i] == "d" && segments[i+1] != "" {
			return segments[i+1], nil
		}
	}

	return "", errors.New("could not extract google document id")
}

func collectBlocks(doc *docs.Document) []Block {
	if doc == nil || doc.Body == nil {
		return nil
	}

	blocks := make([]Block, 0)
	for _, element := range doc.Body.Content {
		appendStructuralElement(&blocks, element)
	}

	return compactBlocks(blocks)
}

func appendStructuralElement(blocks *[]Block, element *docs.StructuralElement) {
	if element == nil {
		return
	}

	if element.Paragraph != nil {
		block := paragraphBlock(element.Paragraph, element.StartIndex, element.EndIndex)
		if strings.TrimSpace(block.Text) != "" {
			*blocks = append(*blocks, block)
		}
		return
	}

	if element.Table != nil {
		for rowIdx, row := range element.Table.TableRows {
			for cellIdx, cell := range row.TableCells {
				for _, content := range cell.Content {
					appendStructuralElement(blocks, content)
				}
				if rowIdx >= 0 && cellIdx >= 0 {
					*blocks = append(*blocks, Block{
						Kind: "table_cell_break",
						Text: "",
						Range: Range{
							StartIndex: element.StartIndex,
							EndIndex:   element.EndIndex,
						},
					})
				}
			}
		}
		return
	}

	if element.TableOfContents != nil {
		for _, content := range element.TableOfContents.Content {
			appendStructuralElement(blocks, content)
		}
	}
}

func paragraphBlock(paragraph *docs.Paragraph, startIndex, endIndex int64) Block {
	block := Block{
		Kind: "paragraph",
		Range: Range{
			StartIndex: startIndex,
			EndIndex:   endIndex,
		},
	}
	if paragraph == nil {
		return block
	}

	var builder strings.Builder
	for _, element := range paragraph.Elements {
		if element == nil || element.TextRun == nil {
			continue
		}
		builder.WriteString(element.TextRun.Content)
	}
	block.Text = strings.TrimSpace(builder.String())

	if paragraph.Bullet != nil {
		block.Kind = "list_item"
		block.ListLevel = int(paragraph.Bullet.NestingLevel)
	}

	if level, isHeading := headingLevel(paragraph.ParagraphStyle); isHeading {
		block.Kind = "heading"
		block.HeadingLevel = level
	}

	return block
}

func headingLevel(style *docs.ParagraphStyle) (int, bool) {
	if style == nil {
		return 0, false
	}

	switch style.NamedStyleType {
	case "TITLE":
		return 0, true
	case "HEADING_1":
		return 1, true
	case "HEADING_2":
		return 2, true
	case "HEADING_3":
		return 3, true
	case "HEADING_4":
		return 4, true
	case "HEADING_5":
		return 5, true
	case "HEADING_6":
		return 6, true
	default:
		return 0, false
	}
}

func compactBlocks(blocks []Block) []Block {
	result := make([]Block, 0, len(blocks))
	for _, block := range blocks {
		if strings.TrimSpace(block.Text) == "" {
			continue
		}
		block.Text = strings.Join(strings.Fields(block.Text), " ")
		result = append(result, block)
	}
	return result
}

func renderBlocksText(blocks []Block) string {
	lines := make([]string, 0, len(blocks))
	for _, block := range blocks {
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		lines = append(lines, text)
	}
	return strings.TrimSpace(strings.Join(lines, "\n\n"))
}

func buildSections(blocks []Block) []Section {
	if len(blocks) == 0 {
		return nil
	}

	sections := make([]Section, 0)
	current := Section{
		ID:    "section_0",
		Title: "Document",
		Level: 0,
		Range: Range{
			StartIndex: blocks[0].Range.StartIndex,
		},
	}
	contentParts := make([]string, 0)

	flush := func(endIndex int64) {
		if strings.TrimSpace(strings.Join(contentParts, "\n\n")) == "" {
			return
		}
		current.Content = strings.TrimSpace(strings.Join(contentParts, "\n\n"))
		current.Range.EndIndex = endIndex
		sections = append(sections, current)
	}

	sectionCounter := 0
	for _, block := range blocks {
		if block.Kind == "heading" {
			if len(contentParts) > 0 {
				flush(block.Range.StartIndex)
				contentParts = contentParts[:0]
			}
			sectionCounter++
			current = Section{
				ID:    fmt.Sprintf("section_%d", sectionCounter),
				Title: block.Text,
				Level: block.HeadingLevel,
				Range: Range{
					StartIndex: block.Range.StartIndex,
				},
			}
			contentParts = append(contentParts, block.Text)
			continue
		}

		contentParts = append(contentParts, block.Text)
	}

	if len(contentParts) > 0 {
		flush(blocks[len(blocks)-1].Range.EndIndex)
	}

	annotateBlocksWithSections(blocks, sections)
	return sections
}

func annotateBlocksWithSections(blocks []Block, sections []Section) {
	for sectionIdx := range sections {
		section := &sections[sectionIdx]
		for blockIdx := range blocks {
			block := &blocks[blockIdx]
			if block.Range.StartIndex >= section.Range.StartIndex && block.Range.EndIndex <= section.Range.EndIndex {
				block.SectionID = section.ID
				block.SectionTitle = section.Title
			}
		}
	}
}
