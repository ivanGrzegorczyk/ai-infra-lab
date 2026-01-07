package config

import (
	"os"
)

type Config struct {
	OllamaURL string
	Port      string
}

func Load() *Config {
	return &Config{
		OllamaURL: getEnv("OLLAMA_URL", "http://localhost:11434"),
		Port:      getEnv("PORT", "8080"),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
