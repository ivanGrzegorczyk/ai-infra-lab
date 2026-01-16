package config

import (
	"os"
)

type Config struct {
	OllamaURL  string
	GroqAPIKey string
	Port       string
	RedisAddr  string
	QdrantAddr string
}

func Load() *Config {
	return &Config{
		OllamaURL:  getEnv("OLLAMA_URL", "http://localhost:11434"),
		GroqAPIKey: getEnv("GROQ_API_KEY", ""),
		Port:       getEnv("PORT", "8080"),
		RedisAddr:  getEnv("REDIS_ADDR", "redis-service.ai-lab:6379"),
		QdrantAddr: getEnv("QDRANT_ADDR", "qdrant-service.ai-lab:6334"),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
