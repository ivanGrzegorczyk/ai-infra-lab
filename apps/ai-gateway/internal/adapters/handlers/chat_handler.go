package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/infra/metrics"
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

	// Configurar Headers de CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	// Manejar el Preflight (Request tipo OPTIONS)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Validar método
	if r.Method != http.MethodPost {
		metrics.HttpRequestsTotal.WithLabelValues("405", "unknown").Inc()
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	// Decodificar el request
	var chatReq domain.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil {
		metrics.HttpRequestsTotal.WithLabelValues("400", "unknown").Inc()
		http.Error(w, "Request inválido", http.StatusBadRequest)
		return
	}

	// Preparar los Headers para Streaming (SSE)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Llamar al servicio
	resChan, errChan := h.service.ExecuteChat(r.Context(), chatReq)

	// El "Flusher" es el que empuja los datos al cliente inmediatamente
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming no soportado", http.StatusInternalServerError)
		return
	}

	// Loop de streaming
	for {
		select {
		case <-r.Context().Done():
			// Si el cliente cierra la conexión, deja de procesar
			return
		case err := <-errChan:
			if err != nil {
				metrics.HttpRequestsTotal.WithLabelValues("500", chatReq.Model).Inc()
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
				return
			}
		case res, ok := <-resChan:
			if !ok {
				metrics.HttpRequestsTotal.WithLabelValues("200", chatReq.Model).Inc()
				// El canal se cerró, termina el stream con un mensaje de fin
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}

			// Si es el primer token, calcula el TTFT
			if isFirstToken {
				duration := time.Since(startTime).Seconds()
				metrics.TimeToFirstToken.WithLabelValues(chatReq.Model, res.Provider).Observe(duration)
				isFirstToken = false
			}

			// Incrementa el contador total de tokens
			metrics.TokensTotal.WithLabelValues(chatReq.Model, res.Provider).Inc()

			// Envia el token en formato SSE
			// Formato: data: {"id": "...", "content": "..."} \n\n
			jsonData, _ := json.Marshal(res)
			fmt.Fprintf(w, "data: %s\n\n", jsonData)

			// Fuerza el envío del buffer al cliente
			flusher.Flush()
		}
	}
}
