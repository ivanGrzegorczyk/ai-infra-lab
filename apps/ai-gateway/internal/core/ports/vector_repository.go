package ports

import (
	"context"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
)

type VectorStore interface {
	// EnsureCollection verifica si existe la "tabla" y si no la crea con las dimensiones correctas
	EnsureCollection(ctx context.Context, name string, vectorSize uint64) error

	// Upsert guarda o actualiza documentos
	Upsert(ctx context.Context, collectionName string, docs []domain.VectorDocument) error

	// Search busca los vectores más similares
	Search(ctx context.Context, collectionName string, vector []float32, limit uint64) ([]domain.SearchResult, error)
}
