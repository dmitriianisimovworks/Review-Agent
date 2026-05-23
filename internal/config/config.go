package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
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

	if !strings.HasPrefix(cfg.HTTP.Address, ":") {
		issues = append(issues, "HTTP_ADDRESS must be in the form ':8080'")
	}

	validateURL(&issues, "DATABASE_URL", cfg.DB.URL)
	validateURL(&issues, "REDIS_URL", cfg.Redis.URL)
	validateURL(&issues, "LLM_BASE_URL", cfg.LLM.BaseURL)

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
