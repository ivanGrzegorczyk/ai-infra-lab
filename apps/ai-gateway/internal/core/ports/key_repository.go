package ports

import "github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"

type KeyRepository interface {
	GetByKey(key string) (domain.APIKeyConfig, bool)
}
