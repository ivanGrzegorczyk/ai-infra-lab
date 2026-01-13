package http

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/observability"
)

type ChatHandler struct {
	service ports.ChatService
}

func NewChatHandler(service ports.ChatService) *ChatHandler {
	return &ChatHandler{service: service}
}

func (h *ChatHandler) Handle(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	isFirstToken := true
	provider := "unknown"
	tokenCount := 0

	// Validar método
	if r.Method != http.MethodPost {
		observability.HttpRequestsTotal.WithLabelValues("405", "unknown", "gateway").Inc()
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	// 1. Extraer la configuración de la API Key del contexto (inyectada por el Middleware)
	keyConfig, ok := r.Context().Value(APIKeyConfigKey).(domain.APIKeyConfig)
	if !ok {
		http.Error(w, "Error interno: Configuración de usuario no encontrada", http.StatusInternalServerError)
		return
	}

	// Decodificar el request
	var chatReq domain.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil {
		observability.HttpRequestsTotal.WithLabelValues("400", "unknown", keyConfig.Name).Inc()
		http.Error(w, "Request inválido", http.StatusBadRequest)
		return
	}

	// Preparar los Headers para Streaming (SSE)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// El "Flusher" es el que empuja los datos al cliente inmediatamente
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming no soportado", http.StatusInternalServerError)
		return
	}

	resChan := make(chan domain.ChatResponse)
	errChan := make(chan error, 1)

	go func() {
		defer close(resChan)
		err := h.service.ExecuteChat(r.Context(), chatReq, keyConfig, resChan)
		if err != nil {
			errChan <- err
		}
	}()

	log.Printf("Iniciando stream para usuario: %s, preferencia: %s", keyConfig.Name, chatReq.PreferredProvider)

	for {
		select {
		case err := <-errChan:
			if err != nil {
				observability.HttpRequestsTotal.WithLabelValues("500", keyConfig.Name, provider).Inc()
				// Si falla, envia evento de error al frontend
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
				flusher.Flush()
				return
			}
		case res, ok := <-resChan:
			if !ok {
				log.Printf("Stream finalizado. Usuario: %s", keyConfig.Name)

				// Métricas finales
				observability.TokensPerRequest.WithLabelValues(keyConfig.Name, provider).Observe(float64(tokenCount))
				observability.TokensTotal.WithLabelValues(keyConfig.Name, provider).Add(float64(tokenCount))
				observability.HttpRequestsTotal.WithLabelValues("200", keyConfig.Name, provider).Inc()

				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}

			// Lógica de métricas y actualización de provider
			provider = res.Provider
			if isFirstToken {
				duration := time.Since(startTime).Seconds()
				observability.TimeToFirstToken.WithLabelValues(keyConfig.Name, provider).Observe(duration)
				isFirstToken = false
			}

			if res.Provider != "" {
				provider = res.Provider
			}
			tokenCount++

			// Enviar payload JSON
			data, _ := json.Marshal(res)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
