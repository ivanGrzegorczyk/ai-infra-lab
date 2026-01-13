package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/adapter/driven/llm"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/adapter/driven/session"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/adapter/driven/storage"
	httpHandler "github.com/ivanGrzegorczyk/ai-infra-gateway/internal/adapter/driving/http"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/config"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/services"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	cfg := config.Load()

	// --- Adaptadores de Salida ---
	ollamaClient := llm.NewOllamaClient(cfg.OllamaURL)
	groqClient := llm.NewGroqClient(cfg.GroqAPIKey)

	// Repositorio de API Keys
	keyRepo, err := storage.NewJSONKeyRepository("configs/keys.json")
	if err != nil {
		log.Fatalf("No se pudo cargar el repositorio de keys: %v", err)
	}

	// Redis
	sessionRepo := session.NewRedisSessionAdapter(cfg.RedisAddr)

	chatService := services.NewChatService(ollamaClient, groqClient, sessionRepo)

	// --- Adaptadores de Entrada ---
	chatHandler := httpHandler.NewChatHandler(chatService)

	// Multiplexer (Router) local
	mux := http.NewServeMux()

	// Ruta de métricas (Pública)
	mux.Handle("/metrics", promhttp.Handler())

	// Ruta de Chat (Protegida por Middleware)
	baseHandler := http.HandlerFunc(chatHandler.Handle)

	// Se envuelve con Auth
	authWrapped := httpHandler.AuthMiddleware(keyRepo)(baseHandler)

	// Se envuelve con CORS (que debe ser lo primero que vea la petición)
	finalHandler := httpHandler.CORSMiddleware(authWrapped)

	// Registra en el mux
	mux.Handle("/v1/chat", finalHandler)

	// --- Servidor ---
	serverAddr := ":" + cfg.Port
	fmt.Printf("AI Gateway corriendo en %s\n", serverAddr)
	fmt.Printf("Providers listos: Ollama (%s) y Groq\n", cfg.OllamaURL)

	log.Fatal(http.ListenAndServe(serverAddr, mux))
}
