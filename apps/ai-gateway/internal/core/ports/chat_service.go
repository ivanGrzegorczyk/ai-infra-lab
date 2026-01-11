package ports

import (
	"context"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
)

// ChatService orquesta la lógica de ruteo y fallbacks.
type ChatService interface {
	ExecuteChat(ctx context.Context, req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error)
}
