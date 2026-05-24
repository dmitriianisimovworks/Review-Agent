package reviewconfig

import (
	"os"
	"path/filepath"
	"testing"

	"technical-specification-review-agent/internal/comment"
	"technical-specification-review-agent/internal/domain"
)

func TestLoaderUsesDefaultsWhenConfigFileMissing(t *testing.T) {
	loader := NewLoader(filepath.Join(t.TempDir(), DefaultPath), Defaults{
		ChunkSize: 4096,
		MaxChunks: 9,
	})

	settings, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if settings.ChunkSize != 4096 {
		t.Fatalf("expected default chunk size 4096, got %d", settings.ChunkSize)
	}
	if settings.MaxChunks != 9 {
		t.Fatalf("expected default max chunks 9, got %d", settings.MaxChunks)
	}
	if !settings.MemoryEnabled {
		t.Fatalf("expected memory to be enabled by default")
	}
	if settings.PublishMode() != comment.PublishModeBoth {
		t.Fatalf("expected default publish mode both, got %s", settings.PublishMode())
	}
	if len(settings.Roles) != len(domain.DefaultReviewerRoles()) {
		t.Fatalf("expected default roles to be enabled")
	}
}

func TestLoaderAppliesYamlOverrides(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, DefaultPath)
	content := `
review:
  architecture: false
  backend: true
  frontend: false
  devops: false
  qa: true
severity:
  critical_block_merge: false
comments:
  inline: false
  summary: true
context:
  memory_enabled: false
document:
  chunk_size: 2048
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loader := NewLoader(path, Defaults{
		ChunkSize: 5000,
		MaxChunks: 12,
	})

	settings, err := loader.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if settings.ChunkSize != 2048 {
		t.Fatalf("expected overridden chunk size 2048, got %d", settings.ChunkSize)
	}
	if settings.MemoryEnabled {
		t.Fatalf("expected memory to be disabled")
	}
	if settings.CriticalBlockMerge {
		t.Fatalf("expected critical_block_merge to be disabled")
	}
	if settings.PublishMode() != comment.PublishModeSummary {
		t.Fatalf("expected summary publish mode, got %s", settings.PublishMode())
	}

	expectedRoles := []domain.ReviewerRole{
		domain.ReviewerRoleSeniorBackendEngineer,
		domain.ReviewerRoleQAReviewer,
	}
	if len(settings.Roles) != len(expectedRoles) {
		t.Fatalf("expected %d roles, got %d", len(expectedRoles), len(settings.Roles))
	}
	for idx, role := range expectedRoles {
		if settings.Roles[idx] != role {
			t.Fatalf("expected role %d to be %s, got %s", idx, role, settings.Roles[idx])
		}
	}
}

func TestLoaderRejectsUnknownReviewKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), DefaultPath)
	content := `
review:
  architecture: true
  mobile: true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	loader := NewLoader(path, Defaults{
		ChunkSize: 5000,
		MaxChunks: 12,
	})

	if _, err := loader.Load(); err == nil {
		t.Fatalf("expected unknown field error for unsupported mobile role")
	}
}
