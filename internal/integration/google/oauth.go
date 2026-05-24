package google

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"

	"technical-specification-review-agent/internal/config"
	"technical-specification-review-agent/internal/domain"
)

type OAuthService struct {
	config     *oauth2.Config
	httpClient *http.Client
}

type googleUserInfo struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func NewOAuthService(cfg config.GoogleConfig) *OAuthService {
	return &OAuthService{
		config: &oauth2.Config{
			ClientID:     cfg.OAuthClientID,
			ClientSecret: cfg.OAuthClientSecret,
			RedirectURL:  cfg.OAuthRedirectURL,
			Scopes:       cfg.OAuthScopes,
			Endpoint:     googleoauth.Endpoint,
		},
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (s *OAuthService) AuthCodeURL(state string) string {
	return s.config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
}

func (s *OAuthService) ExchangeCode(ctx context.Context, code string) (domain.GoogleOAuthConnection, error) {
	token, err := s.config.Exchange(ctx, strings.TrimSpace(code))
	if err != nil {
		return domain.GoogleOAuthConnection{}, fmt.Errorf("exchange oauth code: %w", err)
	}

	userInfo, err := s.fetchUserInfo(ctx, token.AccessToken)
	if err != nil {
		return domain.GoogleOAuthConnection{}, err
	}

	var expiry *time.Time
	if !token.Expiry.IsZero() {
		value := token.Expiry.UTC()
		expiry = &value
	}

	return domain.GoogleOAuthConnection{
		ID:           fmt.Sprintf("google_oauth_%d", time.Now().UTC().UnixNano()),
		GoogleUserID: userInfo.ID,
		Email:        userInfo.Email,
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		Expiry:       expiry,
	}, nil
}

func (s *OAuthService) GenerateState() (string, error) {
	data := make([]byte, 24)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func (s *OAuthService) fetchUserInfo(ctx context.Context, accessToken string) (googleUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return googleUserInfo{}, fmt.Errorf("build userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return googleUserInfo{}, fmt.Errorf("request userinfo: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return googleUserInfo{}, fmt.Errorf("read userinfo response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return googleUserInfo{}, fmt.Errorf("userinfo status %d", resp.StatusCode)
	}

	var user googleUserInfo
	if err := json.Unmarshal(body, &user); err != nil {
		return googleUserInfo{}, fmt.Errorf("decode userinfo response: %w", err)
	}
	if strings.TrimSpace(user.ID) == "" || strings.TrimSpace(user.Email) == "" {
		return googleUserInfo{}, fmt.Errorf("userinfo response is incomplete")
	}

	return user, nil
}
