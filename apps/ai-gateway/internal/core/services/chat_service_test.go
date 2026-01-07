package services

import (
	"context"
	"testing"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
)

type MockLLMProvider struct {
	OnGenerateStream func(req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error)
}

func (m *MockLLMProvider) GenerateStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
	return m.OnGenerateStream(req)
}

func (m *MockLLMProvider) GetName() string {
	return "mock-provider"
}

func TestExecuteChat(t *testing.T) {
	// Setup
	mockProvider := &MockLLMProvider{
		OnGenerateStream: func(req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
			resChan := make(chan domain.ChatResponse, 1)
			errChan := make(chan error, 1)
			
			resChan <- domain.ChatResponse{Content: "Hola desde el mock"}
			close(resChan)
			close(errChan)
			
			return resChan, errChan
		},
	}

	service := NewChatService(mockProvider)
	req := domain.ChatRequest{Model: "test", Stream: true}

	// Ejecución
	resChan, errChan := service.ExecuteChat(context.Background(), req)

	// Validación
	select {
	case res := <-resChan:
		if res.Content != "Hola desde el mock" {
			t.Errorf("Esperaba 'Hola desde el mock', obtuve %s", res.Content)
		}
	case err := <-errChan:
		if err != nil {
			t.Errorf("No esperaba un error, obtuve %v", err)
		}
	}
}
