package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/redis/go-redis/v9"
)

type RedisJobRepository struct {
	client *redis.Client
}

func NewRedisJobRepository(addr string) (*RedisJobRepository, error) {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &RedisJobRepository{client: client}, nil
}

func (r *RedisJobRepository) SaveJob(ctx context.Context, job domain.IngestJob) error {
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	// Guarda el Job por 24 horas
	return r.client.Set(ctx, "job:"+job.ID, data, 24*time.Hour).Err()
}

func (r *RedisJobRepository) GetJob(ctx context.Context, jobID string) (*domain.IngestJob, error) {
	val, err := r.client.Get(ctx, "job:"+jobID).Result()
	if err == redis.Nil {
		return nil, nil // No existe
	}
	if err != nil {
		return nil, err
	}

	var job domain.IngestJob
	err = json.Unmarshal([]byte(val), &job)
	return &job, err
}
