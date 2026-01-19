package config

import (
	"os"
	"strings"
)

type Config struct {
	OllamaURL     string
	GroqAPIKey    string
	Port          string
	RedisAddr     string
	QdrantAddr    string
	Neo4jURI      string
	Neo4jUser     string
	Neo4jPassword string
}

func Load() *Config {
	// Parsear NEO4J_AUTH que viene en formato "username/password"
	neo4jAuth := getEnv("NEO4J_AUTH", "neo4j/password")
	authParts := strings.SplitN(neo4jAuth, "/", 2)

	neo4jUser := "neo4j"
	neo4jPassword := "password"

	if len(authParts) == 2 {
		neo4jUser = authParts[0]
		neo4jPassword = authParts[1]
	}

	return &Config{
		OllamaURL:     getEnv("OLLAMA_URL", "http://localhost:11434"),
		GroqAPIKey:    getEnv("GROQ_API_KEY", ""),
		Port:          getEnv("PORT", "8080"),
		RedisAddr:     getEnv("REDIS_ADDR", "redis-service.ai-lab:6379"),
		QdrantAddr:    getEnv("QDRANT_ADDR", "qdrant-service.ai-lab:6334"),
		Neo4jURI:      getEnv("NEO4J_URI", "bolt://localhost:7687"),
		Neo4jUser:     neo4jUser,
		Neo4jPassword: neo4jPassword,
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
