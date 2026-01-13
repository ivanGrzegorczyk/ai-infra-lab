package ports

import (
	"context"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
)

type SessionRepository interface {
	SaveHistory(ctx context.Context, sessionID string, messages []domain.ChatMessage) error
	GetHistory(ctx context.Context, sessionID string) ([]domain.ChatMessage, error)
	DeleteSession(ctx context.Context, sessionID string) error
}
