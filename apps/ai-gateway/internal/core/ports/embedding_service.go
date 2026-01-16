package ports

import "context"

type EmbeddingGenerator interface {
	// GenerateEmbedding convierte un string en un vector
	GenerateEmbedding(ctx context.Context, text string) ([]float32, error)
}
