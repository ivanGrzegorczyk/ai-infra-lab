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

	// Decodificar el request
	var chatReq domain.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil {
		observability.HttpRequestsTotal.WithLabelValues("400", "unknown", "gateway").Inc()
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

	log.Printf("Iniciando stream para modelo: %s", chatReq.Model)

	for {
		select {
		case <-r.Context().Done():
			// Si el cliente cierra la conexión, deja de procesar
			return
		case err := <-errChan:
			if err != nil {
				log.Println("[Error] Conexión cerrada por el cliente o error en el stream:", err)
				// Si hubo error, usa el último proveedor detectado o "unknown"
				observability.HttpRequestsTotal.WithLabelValues("500", chatReq.Model, provider).Inc()
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
				return
			}
		case res, ok := <-resChan:
			if !ok {
				log.Printf("Stream finalizado exitosamente. Proveedor final: %s", provider)

				// Registra el total de tokens de este mensaje
				observability.TokensPerRequest.WithLabelValues(chatReq.Model, provider).Observe(float64(tokenCount))
				// El canal se cerró con éxito, registra el 200 con el proveedor que atendió la petición
				observability.HttpRequestsTotal.WithLabelValues("200", chatReq.Model, provider).Inc()

				// Termina el stream con un mensaje de fin
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}

			// Actualiza el proveedor con los datos reales de la respuesta
			provider = res.Provider

			// Si es el primer token, calcula el TTFT
			if isFirstToken {
				duration := time.Since(startTime).Seconds()
				observability.TimeToFirstToken.WithLabelValues(chatReq.Model, provider).Observe(duration)
				isFirstToken = false
			}

			// Asegura que siempre haya un proveedor válido
			if res.Provider != "" {
				provider = res.Provider
			} else {
				log.Println("[WARN] Recibido token con Provider VACÍO")
			}

			tokenCount++

			// Incrementa el contador total de tokens
			observability.TokensTotal.WithLabelValues(chatReq.Model, provider).Inc()

			// Envia el token en formato SSE
			jsonData, _ := json.Marshal(res)
			fmt.Fprintf(w, "data: %s\n\n", jsonData)

			// Fuerza el envío del buffer al cliente
			flusher.Flush()
		}
	}
}
