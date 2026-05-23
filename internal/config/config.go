package config

import "os"

type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	LLM      LLMConfig
	Redis    RedisConfig
	DB       DatabaseConfig
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

func Load() Config {
	return Config{
		App: AppConfig{
			Name:             getEnv("APP_NAME", "technical-specification-review-agent"),
			Environment:      getEnv("APP_ENV", "development"),
			InternalAPIToken: getEnv("APP_INTERNAL_API_TOKEN", ""),
			EncryptionKey:    getEnv("APP_ENCRYPTION_KEY", ""),
		},
		HTTP: HTTPConfig{
			Address: getEnv("HTTP_ADDRESS", ":8080"),
		},
		LLM: LLMConfig{
			Provider: getEnv("LLM_PROVIDER", "openai_compatible"),
			BaseURL:  getEnv("LLM_BASE_URL", ""),
			APIKey:   getEnv("LLM_API_KEY", ""),
			Model:    getEnv("LLM_MODEL", ""),
			Timeout:  getEnvInt("LLM_TIMEOUT_SECONDS", 90),
		},
		Redis: RedisConfig{
			URL: getEnv("REDIS_URL", ""),
		},
		DB: DatabaseConfig{
			URL: getEnv("DATABASE_URL", ""),
		},
		Document: DocumentConfig{
			ChunkSize: getEnvInt("DOCUMENT_CHUNK_SIZE", 5000),
			MaxChunks: getEnvInt("DOCUMENT_MAX_CHUNKS", 12),
		},
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
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
