package reviewconfig

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"technical-specification-review-agent/internal/comment"
	"technical-specification-review-agent/internal/domain"
)

const DefaultPath = ".ai-spec-review.yml"

type Provider interface {
	Load() (Settings, error)
}

type Defaults struct {
	ChunkSize int
	MaxChunks int
}

type Settings struct {
	Roles                      []domain.ReviewerRole
	CriticalBlockMerge         bool
	CrossSectionContradictions bool
	InlineComments             bool
	SummaryComments            bool
	MemoryEnabled              bool
	ChunkSize                  int
	MaxChunks                  int
}

type Loader struct {
	path     string
	defaults Defaults
}

type rawConfig struct {
	Review struct {
		Architecture               *bool `yaml:"architecture"`
		Backend                    *bool `yaml:"backend"`
		Frontend                   *bool `yaml:"frontend"`
		DevOps                     *bool `yaml:"devops"`
		QA                         *bool `yaml:"qa"`
		CrossSectionContradictions *bool `yaml:"cross_section_contradictions"`
	} `yaml:"review"`
	Severity struct {
		CriticalBlockMerge *bool `yaml:"critical_block_merge"`
	} `yaml:"severity"`
	Comments struct {
		Inline  *bool `yaml:"inline"`
		Summary *bool `yaml:"summary"`
	} `yaml:"comments"`
	Context struct {
		MemoryEnabled *bool `yaml:"memory_enabled"`
	} `yaml:"context"`
	Document struct {
		ChunkSize *int `yaml:"chunk_size"`
	} `yaml:"document"`
}

func NewLoader(path string, defaults Defaults) *Loader {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath
	}

	return &Loader{
		path:     path,
		defaults: defaults,
	}
}

func (l *Loader) Load() (Settings, error) {
	settings := defaultSettings(l.defaults)

	content, err := os.ReadFile(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return settings, nil
		}
		return Settings{}, fmt.Errorf("read review config: %w", err)
	}

	var raw rawConfig
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&raw); err != nil {
		return Settings{}, fmt.Errorf("parse review config: %w", err)
	}

	if raw.Document.ChunkSize != nil {
		if *raw.Document.ChunkSize < 500 {
			return Settings{}, errors.New("review config document.chunk_size must be >= 500")
		}
		settings.ChunkSize = *raw.Document.ChunkSize
	}

	if raw.Severity.CriticalBlockMerge != nil {
		settings.CriticalBlockMerge = *raw.Severity.CriticalBlockMerge
	}
	if raw.Review.CrossSectionContradictions != nil {
		settings.CrossSectionContradictions = *raw.Review.CrossSectionContradictions
	}
	if raw.Context.MemoryEnabled != nil {
		settings.MemoryEnabled = *raw.Context.MemoryEnabled
	}
	if raw.Comments.Inline != nil {
		settings.InlineComments = *raw.Comments.Inline
	}
	if raw.Comments.Summary != nil {
		settings.SummaryComments = *raw.Comments.Summary
	}
	if !settings.InlineComments && !settings.SummaryComments {
		return Settings{}, errors.New("review config comments must enable at least one of inline or summary")
	}

	settings.Roles = resolveRoles(raw, settings.Roles)
	if len(settings.Roles) == 0 {
		return Settings{}, errors.New("review config review must enable at least one role")
	}

	return settings, nil
}

func (s Settings) PublishMode() comment.PublishMode {
	switch {
	case s.InlineComments && s.SummaryComments:
		return comment.PublishModeBoth
	case s.InlineComments:
		return comment.PublishModeInline
	default:
		return comment.PublishModeSummary
	}
}

func defaultSettings(defaults Defaults) Settings {
	chunkSize := defaults.ChunkSize
	if chunkSize < 500 {
		chunkSize = 5000
	}
	maxChunks := defaults.MaxChunks
	if maxChunks < 1 {
		maxChunks = 12
	}

	return Settings{
		Roles:                      domain.DefaultReviewerRoles(),
		CriticalBlockMerge:         true,
		CrossSectionContradictions: false,
		InlineComments:             true,
		SummaryComments:            true,
		MemoryEnabled:              true,
		ChunkSize:                  chunkSize,
		MaxChunks:                  maxChunks,
	}
}

func resolveRoles(raw rawConfig, fallback []domain.ReviewerRole) []domain.ReviewerRole {
	type toggle struct {
		value *bool
		roles []domain.ReviewerRole
	}

	toggles := []toggle{
		{value: raw.Review.Architecture, roles: []domain.ReviewerRole{domain.ReviewerRoleTechLead, domain.ReviewerRoleSolutionArchitect}},
		{value: raw.Review.Backend, roles: []domain.ReviewerRole{domain.ReviewerRoleSeniorBackendEngineer}},
		{value: raw.Review.Frontend, roles: []domain.ReviewerRole{domain.ReviewerRoleSeniorFrontendEngineer}},
		{value: raw.Review.DevOps, roles: []domain.ReviewerRole{domain.ReviewerRoleDevOpsReviewer}},
		{value: raw.Review.QA, roles: []domain.ReviewerRole{domain.ReviewerRoleQAReviewer}},
	}

	hasExplicitSelection := false
	for _, toggle := range toggles {
		if toggle.value != nil {
			hasExplicitSelection = true
			break
		}
	}
	if !hasExplicitSelection {
		return fallback
	}

	enabled := make([]domain.ReviewerRole, 0, len(fallback))
	seen := make(map[domain.ReviewerRole]struct{}, len(fallback))
	for _, toggle := range toggles {
		if toggle.value == nil || !*toggle.value {
			continue
		}
		for _, role := range toggle.roles {
			if _, exists := seen[role]; exists {
				continue
			}
			seen[role] = struct{}{}
			enabled = append(enabled, role)
		}
	}

	return enabled
}

var _ Provider = (*Loader)(nil)
