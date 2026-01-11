package storage

import (
	"encoding/json"
	"os"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
)

type jsonKeyRepository struct {
	keys map[string]domain.APIKeyConfig
}

// Fíjate que devolvemos el Puerto (interface)
func NewJSONKeyRepository(path string) (ports.KeyRepository, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var list struct {
		Keys []domain.APIKeyConfig `json:"keys"`
	}
	if err := json.Unmarshal(file, &list); err != nil {
		return nil, err
	}

	repo := &jsonKeyRepository{
		keys: make(map[string]domain.APIKeyConfig),
	}

	for _, k := range list.Keys {
		repo.keys[k.Key] = k
	}

	return repo, nil
}

func (r *jsonKeyRepository) GetByKey(key string) (domain.APIKeyConfig, bool) {
	config, ok := r.keys[key]
	return config, ok
}
