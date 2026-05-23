package config

import "os"

type Config struct {
	App   AppConfig
	HTTP  HTTPConfig
	LLM   LLMConfig
	Redis RedisConfig
	DB    DatabaseConfig
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
}

type RedisConfig struct {
	URL string
}

type DatabaseConfig struct {
	URL string
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
		},
		Redis: RedisConfig{
			URL: getEnv("REDIS_URL", ""),
		},
		DB: DatabaseConfig{
			URL: getEnv("DATABASE_URL", ""),
		},
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
