package ports

import (
	"context"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
)

// LLMProvider define lo que un proveedor de IA debe ser capaz de hacer.
type LLMProvider interface {
	// GenerateStream recibe un request y devuelve un canal de respuestas (tokens).
	GenerateStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error)
	GetName() string
}
