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

func createMockProvider(content string) *MockLLMProvider {
	return &MockLLMProvider{
		OnGenerateStream: func(req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
			resChan := make(chan domain.ChatResponse, 1)
			errChan := make(chan error, 1)

			resChan <- domain.ChatResponse{Content: content}
			close(resChan)
			close(errChan)

			return resChan, errChan
		},
	}
}

func TestExecuteChat(t *testing.T) {
	// Setup
	mockLocalProvider := createMockProvider("Hola desde el mock local")
	mockExternalProvider := createMockProvider("Hola desde el mock externo")

	service := NewChatService(mockLocalProvider, mockExternalProvider)

	req := domain.ChatRequest{
		Messages: []domain.ChatMessage{
			{Role: "user", Content: "Test message"},
		},
	}

	keyConfig := domain.APIKeyConfig{
		Key:              "test-key",
		Name:             "test-user",
		AllowedProviders: []string{"ollama", "groq"},
	}

	// Ejecución
	resChan, errChan := service.ExecuteChat(context.Background(), req, keyConfig)

	// Validación
	select {
	case res := <-resChan:
		if res.Content != "Hola desde el mock local" {
			t.Errorf("Esperaba 'Hola desde el mock local', obtuve %s", res.Content)
		}
	case err := <-errChan:
		if err != nil {
			t.Errorf("No esperaba un error, obtuve %v", err)
		}
	}
}
