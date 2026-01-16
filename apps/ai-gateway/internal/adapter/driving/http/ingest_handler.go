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
	// Extraer ID de la URL (asumimos /v1/ingest/{id})
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 4 {
		http.Error(w, "Invalid Job ID", http.StatusBadRequest)
		return
	}
	jobID := pathParts[3]

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
