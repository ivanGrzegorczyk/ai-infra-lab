package services

import (
	"context"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
)

type chatService struct {
	provider ports.LLMProvider
}

// NewChatService es el constructor (Dependency Injection)
func NewChatService(provider ports.LLMProvider) ports.ChatService {
	return &chatService{
		provider: provider,
	}
}

func (s *chatService) ExecuteChat(ctx context.Context, req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
	// TODO: Lógica de fallbacks entre múltiples proveedores
	return s.provider.GenerateStream(ctx, req)
}
