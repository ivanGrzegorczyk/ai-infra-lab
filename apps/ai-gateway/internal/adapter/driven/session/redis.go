package session

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
	"github.com/redis/go-redis/v9"
)

type redisSessionAdapter struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedisSessionAdapter(addr string) ports.SessionRepository {
	rdb := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	return &redisSessionAdapter{
		client: rdb,
		ttl:    24 * time.Hour, // Sesión expira tras 24hs de inactividad
	}
}

func (a *redisSessionAdapter) SaveHistory(ctx context.Context, sessionID string, messages []domain.ChatMessage) error {
	data, err := json.Marshal(messages)
	if err != nil {
		return err
	}
	return a.client.Set(ctx, "session:"+sessionID, data, a.ttl).Err()
}

func (a *redisSessionAdapter) GetHistory(ctx context.Context, sessionID string) ([]domain.ChatMessage, error) {
	val, err := a.client.Get(ctx, "session:"+sessionID).Result()
	if err == redis.Nil {
		return []domain.ChatMessage{}, nil
	}
	if err != nil {
		return nil, err
	}

	var messages []domain.ChatMessage
	err = json.Unmarshal([]byte(val), &messages)
	return messages, err
}

func (a *redisSessionAdapter) DeleteSession(ctx context.Context, sessionID string) error {
	return a.client.Del(ctx, "session:"+sessionID).Err()
}
