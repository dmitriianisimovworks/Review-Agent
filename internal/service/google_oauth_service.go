package service

import (
	"context"
	"fmt"
	"sync"

	"technical-specification-review-agent/internal/apperrors"
	"technical-specification-review-agent/internal/domain"
	"technical-specification-review-agent/internal/repository"
)

type GoogleOAuthProvider interface {
	GenerateState() (string, error)
	AuthCodeURL(state string) string
	ExchangeCode(ctx context.Context, code string) (domain.GoogleOAuthConnection, error)
}

type GoogleOAuthService struct {
	repo        repository.GoogleOAuthConnectionRepository
	oauth       GoogleOAuthProvider
	stateMu     sync.Mutex
	validStates map[string]struct{}
}

func NewGoogleOAuthService(repo repository.GoogleOAuthConnectionRepository, oauth GoogleOAuthProvider) *GoogleOAuthService {
	return &GoogleOAuthService{
		repo:        repo,
		oauth:       oauth,
		validStates: make(map[string]struct{}),
	}
}

func (s *GoogleOAuthService) BeginAuth() (string, error) {
	state, err := s.oauth.GenerateState()
	if err != nil {
		return "", apperrors.Wrap(apperrors.KindInternal, "failed to generate oauth state", err)
	}

	s.stateMu.Lock()
	s.validStates[state] = struct{}{}
	s.stateMu.Unlock()

	return s.oauth.AuthCodeURL(state), nil
}

func (s *GoogleOAuthService) CompleteAuth(ctx context.Context, state, code string) (domain.GoogleOAuthConnection, error) {
	if !s.consumeState(state) {
		return domain.GoogleOAuthConnection{}, apperrors.New(apperrors.KindInvalidArgument, "invalid oauth state")
	}

	connection, err := s.oauth.ExchangeCode(ctx, code)
	if err != nil {
		return domain.GoogleOAuthConnection{}, apperrors.Wrap(apperrors.KindDependency, "failed to exchange google oauth code", err)
	}

	if err := s.repo.Save(ctx, connection); err != nil {
		return domain.GoogleOAuthConnection{}, apperrors.Wrap(apperrors.KindInternal, "failed to store google oauth connection", err)
	}

	saved, err := s.repo.GetByGoogleUserID(ctx, connection.GoogleUserID)
	if err != nil {
		return domain.GoogleOAuthConnection{}, apperrors.Wrap(apperrors.KindInternal, "failed to load stored google oauth connection", err)
	}

	return saved, nil
}

func (s *GoogleOAuthService) consumeState(state string) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	if _, ok := s.validStates[state]; !ok {
		return false
	}
	delete(s.validStates, state)
	return true
}

func (s *GoogleOAuthService) DebugStateCount() string {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return fmt.Sprintf("%d", len(s.validStates))
}
