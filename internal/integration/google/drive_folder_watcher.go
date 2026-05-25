package google

import (
	"context"
	"fmt"
	"sort"
	"time"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

type DriveFile struct {
	ID           string
	Name         string
	DocumentURL  string
	ModifiedTime time.Time
}

type FolderWatcher interface {
	ListDocuments(ctx context.Context, folderID string) ([]DriveFile, error)
}

type DriveFolderWatcher struct {
	driveService *drive.Service
}

func NewDriveFolderWatcher(ctx context.Context, credentialsFile string) (*DriveFolderWatcher, error) {
	service, err := drive.NewService(ctx, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("create drive folder watcher: %w", err)
	}

	return &DriveFolderWatcher{driveService: service}, nil
}

func (w *DriveFolderWatcher) ListDocuments(ctx context.Context, folderID string) ([]DriveFile, error) {
	files, err := w.driveService.Files.List().
		Q(fmt.Sprintf("'%s' in parents and trashed = false and mimeType = 'application/vnd.google-apps.document'", folderID)).
		Fields("files(id,name,modifiedTime)").
		OrderBy("modifiedTime desc").
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("list drive folder documents: %w", err)
	}

	result := make([]DriveFile, 0, len(files.Files))
	for _, file := range files.Files {
		modifiedAt, err := time.Parse(time.RFC3339, file.ModifiedTime)
		if err != nil {
			modifiedAt = time.Time{}
		}

		result = append(result, DriveFile{
			ID:           file.Id,
			Name:         file.Name,
			DocumentURL:  fmt.Sprintf("https://docs.google.com/document/d/%s/edit", file.Id),
			ModifiedTime: modifiedAt.UTC(),
		})
	}

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].ModifiedTime.Before(result[j].ModifiedTime)
	})
	return result, nil
}
