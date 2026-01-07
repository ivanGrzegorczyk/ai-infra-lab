package clients

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
)

type ollamaClient struct {
	baseURL string
	client  *http.Client
}

func NewOllamaClient(baseURL string) ports.LLMProvider {
	return &ollamaClient{
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

func (c *ollamaClient) GetName() string {
	return "ollama"
}

func (c *ollamaClient) GenerateStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
	resChan := make(chan domain.ChatResponse)
	errChan := make(chan error, 1)

	go func() {
		defer close(resChan)
		defer close(errChan)

		// 1. Traducir al formato de Ollama (su API espera /api/chat)
		url := fmt.Sprintf("%s/api/chat", c.baseURL)
		
		// Ollama usa "messages" pero su estructura es levemente distinta en algunos campos
		// Por ahora mapea lo básico
		body, _ := json.Marshal(map[string]interface{}{
			"model":    req.Model,
			"messages": req.Messages,
			"stream":   req.Stream,
		})

		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
		if err != nil {
			errChan <- err
			return
		}

		resp, err := c.client.Do(httpReq)
		if err != nil {
			errChan <- err
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errChan <- fmt.Errorf("ollama error: status %d", resp.StatusCode)
			return
		}

		// 2. Procesar el Stream línea por línea
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			var ollamaResp struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				Done bool `json:"done"`
			}

			if err := json.Unmarshal(scanner.Bytes(), &ollamaResp); err != nil {
				continue
			}

			resChan <- domain.ChatResponse{
				Content:   ollamaResp.Message.Content,
				Provider:  "ollama",
				CreatedAt: time.Now(),
			}

			if ollamaResp.Done {
				break
			}
		}
	}()

	return resChan, errChan
}
