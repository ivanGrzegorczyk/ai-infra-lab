package ports

import (
	"context"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
)

type JobRepository interface {
	SaveJob(ctx context.Context, job domain.IngestJob) error
	GetJob(ctx context.Context, jobID string) (*domain.IngestJob, error)
}
