package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/services"
)

type IngestHandler struct {
	service *services.IngestService
}

func NewIngestHandler(service *services.IngestService) *IngestHandler {
	return &IngestHandler{service: service}
}

func (h *IngestHandler) HandleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req domain.IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Validar que el contenido no esté vacío
	if strings.TrimSpace(req.Content) == "" {
		http.Error(w, "Content cannot be empty", http.StatusBadRequest)
		return
	}

	// Validar que el contenido sea texto plano válido (UTF-8)
	if !h.isValidTextContent(req.Content) {
		http.Error(w, "Content must be plain text (UTF-8). Binary files are not supported.", http.StatusBadRequest)
		return
	}

	job, err := h.service.StartIngestion(r.Context(), req)
	if err != nil {
		http.Error(w, "Internal Server Error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted) // 202 Accepted es la respuesta correcta para async
	json.NewEncoder(w).Encode(job)
}

func (h *IngestHandler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	// Extraer ID de la URL (formato: /v1/ingest/status/{id})
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 5 {
		http.Error(w, "Invalid Job ID", http.StatusBadRequest)
		return
	}
	jobID := pathParts[4]

	job, err := h.service.GetJobStatus(r.Context(), jobID)
	if err != nil {
		http.Error(w, "Error fetching job", http.StatusInternalServerError)
		return
	}
	if job == nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(job)
}

// isValidTextContent verifica si el contenido es texto plano válido
func (h *IngestHandler) isValidTextContent(content string) bool {
	// Contar caracteres no imprimibles y binarios
	nonPrintable := 0
	total := len(content)

	if total == 0 {
		return false
	}

	// Verificar que sea UTF-8 válido
	for _, r := range content {
		// Permitir caracteres imprimibles, espacios, tabs, saltos de línea
		if r < 32 && r != '\n' && r != '\r' && r != '\t' {
			nonPrintable++
		}
		// Detectar caracteres de control binarios
		if r == 0 || r == 0xFFFD { // NULL byte o replacement character (indica UTF-8 inválido)
			return false
		}
	}

	// Si más del 10% son caracteres no imprimibles, probablemente es binario
	if float64(nonPrintable)/float64(total) > 0.1 {
		return false
	}

	return true
}
