package domain

import "time"

type JobStatus string

const (
	StatusPending    JobStatus = "pending"
	StatusProcessing JobStatus = "processing"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)

type IngestJob struct {
	ID        string                 `json:"id"`
	Status    JobStatus              `json:"status"`
	Message   string                 `json:"message,omitempty"` // Para errores o info
	CreatedAt time.Time              `json:"created_at"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type IngestRequest struct {
	Content  string                 `json:"content"` // Texto a vectorizar
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}
