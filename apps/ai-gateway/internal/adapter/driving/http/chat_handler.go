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

	// Llamar al servicio pasando la configuración del usuario
	resChan, errChan := h.service.ExecuteChat(r.Context(), chatReq, keyConfig)

	// El "Flusher" es el que empuja los datos al cliente inmediatamente
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming no soportado", http.StatusInternalServerError)
		return
	}

	log.Printf("Iniciando stream para usuario: %s, preferencia: %s", keyConfig.Name, chatReq.PreferredProvider)

	for {
		select {
		case <-r.Context().Done():
			// Si el cliente cierra la conexión, deja de procesar
			return
		case err := <-errChan:
			if err != nil {
				log.Println("[Error] Conexión cerrada por el cliente o error en el stream:", err)
				// Si hubo error, usa el último proveedor detectado o "unknown"
				observability.HttpRequestsTotal.WithLabelValues("500", keyConfig.Name, provider).Inc()
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
				return
			}
		case res, ok := <-resChan:
			if !ok {
				log.Printf("Stream finalizado exitosamente. Usuario: %s, Proveedor: %s", keyConfig.Name, provider)

				// Registra métricas del stream completado
				observability.TokensPerRequest.WithLabelValues(keyConfig.Name, provider).Observe(float64(tokenCount))
				observability.TokensTotal.WithLabelValues(keyConfig.Name, provider).Add(float64(tokenCount))
				observability.HttpRequestsTotal.WithLabelValues("200", keyConfig.Name, provider).Inc()

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
				observability.TimeToFirstToken.WithLabelValues(keyConfig.Name, provider).Observe(duration)
				isFirstToken = false
			}

			// Asegura que siempre haya un proveedor válido
			if res.Provider != "" {
				provider = res.Provider
			} else {
				log.Println("[WARN] Recibido token con Provider VACÍO")
			}

			tokenCount++

			// Envia el token en formato SSE
			jsonData, _ := json.Marshal(res)
			fmt.Fprintf(w, "data: %s\n\n", jsonData)

			// Fuerza el envío del buffer al cliente
			flusher.Flush()
		}
	}
}
