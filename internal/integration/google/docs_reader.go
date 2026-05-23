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

	return Document{
		ExternalID: documentID,
		Title:      title,
		Content:    renderDocumentText(doc),
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

func renderDocumentText(doc *docs.Document) string {
	if doc == nil || doc.Body == nil {
		return ""
	}

	var lines []string
	for _, element := range doc.Body.Content {
		appendStructuralElement(&lines, element)
	}

	return strings.TrimSpace(strings.Join(compactLines(lines), "\n"))
}

func appendStructuralElement(lines *[]string, element *docs.StructuralElement) {
	if element == nil {
		return
	}

	if element.Paragraph != nil {
		text := paragraphText(element.Paragraph)
		if text != "" {
			*lines = append(*lines, text)
		}
		return
	}

	if element.Table != nil {
		for _, row := range element.Table.TableRows {
			for _, cell := range row.TableCells {
				for _, content := range cell.Content {
					appendStructuralElement(lines, content)
				}
			}
		}
		return
	}

	if element.TableOfContents != nil {
		for _, content := range element.TableOfContents.Content {
			appendStructuralElement(lines, content)
		}
	}
}

func paragraphText(paragraph *docs.Paragraph) string {
	if paragraph == nil {
		return ""
	}

	var builder strings.Builder
	for _, element := range paragraph.Elements {
		if element == nil || element.TextRun == nil {
			continue
		}
		builder.WriteString(element.TextRun.Content)
	}

	return strings.TrimSpace(builder.String())
}

func compactLines(lines []string) []string {
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(result) == 0 || result[len(result)-1] == "" {
				continue
			}
			result = append(result, "")
			continue
		}
		result = append(result, strings.Join(strings.Fields(trimmed), " "))
	}
	return result
}

