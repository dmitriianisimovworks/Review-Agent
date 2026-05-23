package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	LLM      LLMConfig
	Redis    RedisConfig
	DB       DatabaseConfig
	Cache    CacheConfig
	Document DocumentConfig
	Google   GoogleConfig
}

type AppConfig struct {
	Name             string
	Environment      string
	InternalAPIToken string
	EncryptionKey    string
}

type HTTPConfig struct {
	Address string
}

type LLMConfig struct {
	Provider string
	BaseURL  string
	APIKey   string
	Model    string
	Timeout  int
}

type RedisConfig struct {
	URL string
}

type DatabaseConfig struct {
	URL string
}

type DocumentConfig struct {
	ChunkSize int
	MaxChunks int
}

type CacheConfig struct {
	AnalysisTTLSeconds int
}

type GoogleConfig struct {
	ServiceAccountFile string
	OAuthClientID      string
	OAuthClientSecret  string
	OAuthRedirectURL   string
	OAuthScopes        []string
}

func Load() (Config, error) {
	cfg := Config{
		App: AppConfig{
			Name:             getEnvOrDefault("APP_NAME", "technical-specification-review-agent"),
			Environment:      getEnvOrDefault("APP_ENV", "development"),
			InternalAPIToken: getEnv("APP_INTERNAL_API_TOKEN"),
			EncryptionKey:    getEnv("APP_ENCRYPTION_KEY"),
		},
		HTTP: HTTPConfig{
			Address: getEnvOrDefault("HTTP_ADDRESS", ":8080"),
		},
		LLM: LLMConfig{
			Provider: getEnvOrDefault("LLM_PROVIDER", "openai_compatible"),
			BaseURL:  getEnv("LLM_BASE_URL"),
			APIKey:   getEnv("LLM_API_KEY"),
			Model:    getEnv("LLM_MODEL"),
			Timeout:  getEnvInt("LLM_TIMEOUT_SECONDS", 90),
		},
		Redis: RedisConfig{
			URL: getEnv("REDIS_URL"),
		},
		DB: DatabaseConfig{
			URL: getEnv("DATABASE_URL"),
		},
		Cache: CacheConfig{
			AnalysisTTLSeconds: getEnvInt("CACHE_ANALYSIS_TTL_SECONDS", 1800),
		},
		Document: DocumentConfig{
			ChunkSize: getEnvInt("DOCUMENT_CHUNK_SIZE", 5000),
			MaxChunks: getEnvInt("DOCUMENT_MAX_CHUNKS", 12),
		},
		Google: GoogleConfig{
			ServiceAccountFile: getEnv("GOOGLE_SERVICE_ACCOUNT_FILE"),
			OAuthClientID:      getEnv("GOOGLE_OAUTH_CLIENT_ID"),
			OAuthClientSecret:  getEnv("GOOGLE_OAUTH_CLIENT_SECRET"),
			OAuthRedirectURL:   getEnv("GOOGLE_OAUTH_REDIRECT_URL"),
			OAuthScopes:        splitCSV(getEnv("GOOGLE_OAUTH_SCOPES")),
		},
	}

	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func getEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func getEnvOrDefault(key, fallback string) string {
	value := getEnv(key)
	if value != "" {
		return value
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	var parsed int
	for _, char := range value {
		if char < '0' || char > '9' {
			return fallback
		}
		parsed = parsed*10 + int(char-'0')
	}

	if parsed <= 0 {
		return fallback
	}

	return parsed
}

func validate(cfg Config) error {
	var issues []string

	requireNonPlaceholder(&issues, "DATABASE_URL", cfg.DB.URL)
	requireNonPlaceholder(&issues, "REDIS_URL", cfg.Redis.URL)
	requireNonPlaceholder(&issues, "LLM_API_KEY", cfg.LLM.APIKey)
	requireNonPlaceholder(&issues, "LLM_BASE_URL", cfg.LLM.BaseURL)
	requireNonPlaceholder(&issues, "LLM_MODEL", cfg.LLM.Model)
	requireNonPlaceholder(&issues, "APP_INTERNAL_API_TOKEN", cfg.App.InternalAPIToken)
	requireNonPlaceholder(&issues, "APP_ENCRYPTION_KEY", cfg.App.EncryptionKey)
	requireNonPlaceholder(&issues, "GOOGLE_SERVICE_ACCOUNT_FILE", cfg.Google.ServiceAccountFile)
	requireNonPlaceholder(&issues, "GOOGLE_OAUTH_CLIENT_ID", cfg.Google.OAuthClientID)
	requireNonPlaceholder(&issues, "GOOGLE_OAUTH_CLIENT_SECRET", cfg.Google.OAuthClientSecret)
	requireNonPlaceholder(&issues, "GOOGLE_OAUTH_REDIRECT_URL", cfg.Google.OAuthRedirectURL)

	if !strings.HasPrefix(cfg.HTTP.Address, ":") {
		issues = append(issues, "HTTP_ADDRESS must be in the form ':8080'")
	}

	validateURL(&issues, "DATABASE_URL", cfg.DB.URL)
	validateURL(&issues, "REDIS_URL", cfg.Redis.URL)
	validateURL(&issues, "LLM_BASE_URL", cfg.LLM.BaseURL)
	validateURL(&issues, "GOOGLE_OAUTH_REDIRECT_URL", cfg.Google.OAuthRedirectURL)

	if cfg.Document.ChunkSize < 500 {
		issues = append(issues, "DOCUMENT_CHUNK_SIZE must be >= 500")
	}

	if cfg.Document.MaxChunks < 1 {
		issues = append(issues, "DOCUMENT_MAX_CHUNKS must be >= 1")
	}

	if cfg.LLM.Timeout < 5 {
		issues = append(issues, "LLM_TIMEOUT_SECONDS must be >= 5")
	}

	if cfg.Cache.AnalysisTTLSeconds < 30 {
		issues = append(issues, "CACHE_ANALYSIS_TTL_SECONDS must be >= 30")
	}

	if cfg.Google.ServiceAccountFile != "" {
		if !filepath.IsAbs(cfg.Google.ServiceAccountFile) {
			issues = append(issues, "GOOGLE_SERVICE_ACCOUNT_FILE must be an absolute path")
		} else if _, err := os.Stat(cfg.Google.ServiceAccountFile); err != nil {
			issues = append(issues, fmt.Sprintf("GOOGLE_SERVICE_ACCOUNT_FILE is not readable: %v", err))
		}
	}

	if len(cfg.Google.OAuthScopes) == 0 {
		issues = append(issues, "GOOGLE_OAUTH_SCOPES must contain at least one scope")
	}

	if len(issues) == 0 {
		return nil
	}

	return errors.New("invalid config: " + strings.Join(issues, "; "))
}

func requireNonPlaceholder(issues *[]string, key, value string) {
	if value == "" {
		*issues = append(*issues, fmt.Sprintf("%s is required", key))
		return
	}

	lowered := strings.ToLower(strings.TrimSpace(value))
	if lowered == "replace_me" || strings.Contains(lowered, "твой_реальный") {
		*issues = append(*issues, fmt.Sprintf("%s contains a placeholder value", key))
	}
}

func validateURL(issues *[]string, key, value string) {
	if value == "" {
		return
	}
	if _, err := url.Parse(value); err != nil {
		*issues = append(*issues, fmt.Sprintf("%s is not a valid URL: %v", key, err))
	}
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
