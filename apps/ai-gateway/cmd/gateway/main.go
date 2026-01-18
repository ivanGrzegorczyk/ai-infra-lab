package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/adapter/driven/llm"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/adapter/driven/session"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/adapter/driven/storage"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/adapter/driven/vector"
	httpHandler "github.com/ivanGrzegorczyk/ai-infra-gateway/internal/adapter/driving/http"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/config"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/services"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// 1. Cargar configuracion
	cfg := config.Load()

	// ---------------------------------------------------------
	// 2. Inicializar Adaptadores (Infraestructura)
	// ---------------------------------------------------------

	// API Keys
	keyRepo, err := storage.NewJSONKeyRepository("configs/keys.json")
	if err != nil {
		log.Fatalf("Error cargando repository de keys: %v", err)
	}

	// Redis - Sesiones
	sessionRepo := session.NewRedisSessionAdapter(cfg.RedisAddr)

	// Redis - Jobs de Ingesta
	jobRepo, err := storage.NewRedisJobRepository(cfg.RedisAddr)
	if err != nil {
		log.Fatalf("Error conectando a Redis Jobs: %v", err)
	}

	// Qdrant - Vector Store
	qdrantAdapter, err := vector.NewQdrantAdapter(cfg.QdrantAddr)
	if err != nil {
		log.Fatalf("Error conectando a Qdrant: %v", err)
	}
	defer qdrantAdapter.Close()

	// LLM Clients
	ollamaClient := llm.NewOllamaClient(cfg.OllamaURL)
	groqClient := llm.NewGroqClient(cfg.GroqAPIKey)

	// ---------------------------------------------------------
	// 3. Inicializar Servicios (Core)
	// ---------------------------------------------------------

	chatService := services.NewChatService(ollamaClient, groqClient, sessionRepo, ollamaClient, qdrantAdapter)
	ingestService := services.NewIngestService(jobRepo, ollamaClient, qdrantAdapter)

	// ---------------------------------------------------------
	// 4. Inicializar Handlers (Http)
	// ---------------------------------------------------------

	chatHandler := httpHandler.NewChatHandler(chatService)
	ingestHandler := httpHandler.NewIngestHandler(ingestService)

	// ---------------------------------------------------------
	// 5. Router y Middlewares
	// ---------------------------------------------------------

	mux := http.NewServeMux()

	// Endpoints Publicos
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Helper para aplicar middlewares (CORS -> Auth -> Handler)
	protect := func(h http.HandlerFunc) http.Handler {
		// 1. Envuelve el handler con Auth
		authMiddleware := httpHandler.AuthMiddleware(keyRepo)
		authenticatedHandler := authMiddleware(h)

		// 2. Envuelve el resultado con CORS
		return httpHandler.CORSMiddleware(authenticatedHandler)
	}

	// Endpoints Protegidos
	mux.Handle("/v1/chat", protect(chatHandler.Handle))
	mux.Handle("/v1/ingest", protect(ingestHandler.HandleIngest))
	mux.Handle("/v1/ingest/status/", protect(ingestHandler.HandleStatus))

	// ---------------------------------------------------------
	// 6. Iniciar Servidor
	// ---------------------------------------------------------

	serverAddr := ":" + cfg.Port
	fmt.Printf("AI Gateway iniciado en %s\n", serverAddr)
	fmt.Printf("Configuracion: Ollama (%s) | Qdrant (%s)\n", cfg.OllamaURL, cfg.QdrantAddr)

	if err := http.ListenAndServe(serverAddr, mux); err != nil {
		log.Fatalf("Fallo en el servidor: %v", err)
	}
}
