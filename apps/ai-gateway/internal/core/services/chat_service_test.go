package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
)

// --- MOCKS (Para evitar duplicación y dependencias externas) ---

// 1. Mock de LLM Provider (Groq/Ollama)
type MockLLMProvider struct {
	Name             string
	OnGenerateStream func(req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error)
}

func (m *MockLLMProvider) GenerateStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
	return m.OnGenerateStream(req)
}
func (m *MockLLMProvider) GetName() string { return m.Name }

// 2. Mock de Session Repository (Redis)
type MockSessionRepository struct {
	History map[string][]domain.ChatMessage
}

func NewMockSessionRepository() *MockSessionRepository {
	return &MockSessionRepository{History: make(map[string][]domain.ChatMessage)}
}
func (m *MockSessionRepository) SaveHistory(ctx context.Context, sessionID string, messages []domain.ChatMessage) error {
	m.History[sessionID] = messages
	return nil
}
func (m *MockSessionRepository) GetHistory(ctx context.Context, sessionID string) ([]domain.ChatMessage, error) {
	if h, ok := m.History[sessionID]; ok {
		return h, nil
	}
	return []domain.ChatMessage{}, nil
}
func (m *MockSessionRepository) DeleteSession(ctx context.Context, sessionID string) error {
	delete(m.History, sessionID)
	return nil
}

// 3. Mock de Embedding Generator (para RAG)
type MockEmbeddingGenerator struct{}

func (m *MockEmbeddingGenerator) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	// Retorna un vector dummy para el test
	return []float32{0.1, 0.2, 0.3}, nil
}

// 4. Mock de Vector Store (para RAG)
type MockVectorStore struct {
	SearchResults []domain.SearchResult
}

func (m *MockVectorStore) EnsureCollection(ctx context.Context, name string, size uint64) error {
	return nil
}

func (m *MockVectorStore) Upsert(ctx context.Context, collectionName string, docs []domain.VectorDocument) error {
	return nil
}

func (m *MockVectorStore) Search(ctx context.Context, collectionName string, vector []float32, limit uint64) ([]domain.SearchResult, error) {
	// Por defecto, retorna resultados vacíos (no hay contexto RAG)
	return m.SearchResults, nil
}

// --- HELPER DE SETUP (DRY) ---
func setupService() (ports.ChatService, *MockLLMProvider, *MockLLMProvider, *MockSessionRepository, *MockVectorStore) {
	local := &MockLLMProvider{Name: "ollama"}
	external := &MockLLMProvider{Name: "groq"}
	sessionRepo := NewMockSessionRepository()
	embedder := &MockEmbeddingGenerator{}
	vectorStore := &MockVectorStore{SearchResults: []domain.SearchResult{}} // Sin contexto RAG por defecto

	svc := NewChatService(local, external, sessionRepo, embedder, vectorStore)
	return svc, local, external, sessionRepo, vectorStore
}

// Helper para crear streams simulados
func mockStreamResponse(content string, count int) (<-chan domain.ChatResponse, <-chan error) {
	resChan := make(chan domain.ChatResponse, count)
	errChan := make(chan error, 1) // Buffer 1 para no bloquear

	go func() {
		defer close(resChan)
		defer close(errChan)
		for i := 0; i < count; i++ {
			chunk := content
			if count > 1 {
				chunk = "token"
			} // Simular tokens si count > 1
			resChan <- domain.ChatResponse{Content: chunk, Provider: "mock"}
			time.Sleep(1 * time.Millisecond) // Simular latencia mínima
		}
	}()
	return resChan, errChan
}

// --- TESTS ---

func TestExecuteChat_BasicFlow(t *testing.T) {
	svc, local, _, _, _ := setupService()

	// Configurar comportamiento del Mock
	local.OnGenerateStream = func(req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
		return mockStreamResponse("Hola mundo", 1)
	}

	resChan := make(chan domain.ChatResponse)
	go func() {
		defer close(resChan)
		req := domain.ChatRequest{Messages: []domain.ChatMessage{{Role: "user", Content: "Hola"}}}
		cfg := domain.APIKeyConfig{AllowedProviders: []string{"ollama"}}

		err := svc.ExecuteChat(context.Background(), req, cfg, resChan)
		if err != nil {
			t.Errorf("Error inesperado: %v", err)
		}
	}()

	// Verificar salida
	received := ""
	for res := range resChan {
		received += res.Content
	}
	if received != "Hola mundo" {
		t.Errorf("Se esperaba 'Hola mundo', se recibió '%s'", received)
	}
}

func TestExecuteChat_SessionMemory(t *testing.T) {
	svc, local, _, sessionRepo, _ := setupService()

	// 1. Pre-cargar una sesión en el mock de Redis
	sessionID := "sess-123"
	sessionRepo.SaveHistory(context.Background(), sessionID, []domain.ChatMessage{
		{Role: "user", Content: "Me llamo Iván"},
		{Role: "assistant", Content: "Hola Iván"},
	})

	local.OnGenerateStream = func(req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
		// Verificar que el servicio le pasó el historial completo al provider
		// Con RAG puede haber un system message extra si hay contexto, pero en este test no hay contexto
		if len(req.Messages) < 3 { // Al menos 2 previos + 1 nuevo
			t.Errorf("El provider debió recibir al menos 3 mensajes, recibió %d", len(req.Messages))
		}
		return mockStreamResponse("Entendido", 1)
	}

	resChan := make(chan domain.ChatResponse)
	go func() {
		defer close(resChan)
		req := domain.ChatRequest{
			SessionID: sessionID,
			Messages:  []domain.ChatMessage{{Role: "user", Content: "¿Cómo me llamo?"}},
		}
		cfg := domain.APIKeyConfig{AllowedProviders: []string{"ollama"}}
		svc.ExecuteChat(context.Background(), req, cfg, resChan)
	}()

	// Consumir canal para que termine
	for range resChan {
	}

	// 2. Verificar que el NUEVO mensaje y la respuesta se guardaron
	history, _ := sessionRepo.GetHistory(context.Background(), sessionID)
	if len(history) != 4 {
		t.Errorf("El historial final debería tener 4 mensajes, tiene %d", len(history))
	}
}

func TestExecuteChat_SafetyBreak(t *testing.T) {
	svc, _, external, _, _ := setupService()

	// Simular un provider que se vuelve loco y manda 3000 tokens
	external.OnGenerateStream = func(req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
		// 2500 tokens > limite de seguridad (2000)
		return mockStreamResponse("x", 2500)
	}

	resChan := make(chan domain.ChatResponse)
	tokenCount := 0

	go func() {
		defer close(resChan)
		req := domain.ChatRequest{Messages: []domain.ChatMessage{{Role: "user", Content: "Rompeté"}}}
		cfg := domain.APIKeyConfig{AllowedProviders: []string{"groq"}} // Groq suele ser el external
		_ = svc.ExecuteChat(context.Background(), req, cfg, resChan)
	}()

	for range resChan {
		tokenCount++
	}

	// El Safety Break debería cortar en 2001 aprox
	if tokenCount >= 2500 {
		t.Errorf("El Safety Break falló: se recibieron %d tokens (debió cortar antes)", tokenCount)
	} else {
		fmt.Printf("Safety Break funcionó correctamente: cortó en %d tokens\n", tokenCount)
	}
}
