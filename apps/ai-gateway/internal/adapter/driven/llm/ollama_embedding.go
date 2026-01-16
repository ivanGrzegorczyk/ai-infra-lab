package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Estructuras internas para el JSON de embeddings
type embeddingReq struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type embeddingRes struct {
	Embedding []float64 `json:"embedding"`
}

// GenerateEmbedding extiende la funcionalidad de ollamaClient definido en ollama.go
func (c *ollamaClient) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	// Construye la URL usando el campo baseURL que ya existe en ollamaClient
	url := fmt.Sprintf("%s/api/embeddings", c.baseURL)

	reqBody := embeddingReq{
		Model:  "nomic-embed-text",
		Prompt: text,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Usa el http.Client que ya existe en ollamaClient
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("error llamando a ollama embeddings: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embedding status: %d", resp.StatusCode)
	}

	var res embeddingRes
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("error decodificando respuesta embedding: %w", err)
	}

	// Conversión de float64 a float32
	vector := make([]float32, len(res.Embedding))
	for i, v := range res.Embedding {
		vector[i] = float32(v)
	}

	return vector, nil
}
