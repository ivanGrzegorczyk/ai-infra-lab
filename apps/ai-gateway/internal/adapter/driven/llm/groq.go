package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/observability"
)

type groqClient struct {
	apiKey string
	client *http.Client
}

func NewGroqClient(apiKey string) ports.LLMProvider {
	return &groqClient{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

func (c *groqClient) GetName() string { return "groq" }

func (c *groqClient) GenerateStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
	resChan := make(chan domain.ChatResponse)
	errChan := make(chan error, 1)

	go func() {
		defer close(resChan)
		defer close(errChan)

		url := "https://api.groq.com/openai/v1/chat/completions"
		body, _ := json.Marshal(map[string]interface{}{
			"model":    "llama-3.3-70b-versatile",
			"messages": req.Messages,
			"stream":   true,
		})

		httpReq, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(body))
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.client.Do(httpReq)
		if resp != nil {
			remTokens := resp.Header.Get("x-ratelimit-remaining-tokens")
			remReqs := resp.Header.Get("x-ratelimit-remaining-requests")

			if t, parseErr := strconv.ParseFloat(remTokens, 64); parseErr == nil {
				observability.GroqRemainingTokens.Set(t)
			}
			if r, parseErr := strconv.ParseFloat(remReqs, 64); parseErr == nil {
				observability.GroqRemainingRequests.Set(r)
			}
		}

		if err != nil {
			errChan <- err
			return
		}
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var groqResp struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}

			if err := json.Unmarshal([]byte(data), &groqResp); err != nil {
				continue
			}

			if len(groqResp.Choices) > 0 {
				resChan <- domain.ChatResponse{
					Content:  groqResp.Choices[0].Delta.Content,
					Provider: "groq",
				}
			}
		}
	}()
	return resChan, errChan
}
