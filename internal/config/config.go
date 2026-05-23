package config

import "os"

type Config struct {
	HTTP HTTPConfig
}

type HTTPConfig struct {
	Address string
}

func Load() Config {
	return Config{
		HTTP: HTTPConfig{
			Address: getEnv("HTTP_ADDRESS", ":8080"),
		},
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
