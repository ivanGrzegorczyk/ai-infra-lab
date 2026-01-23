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

	// graphRepo se pasa como nil para tests sin GraphRAG
	svc := NewChatService(local, external, sessionRepo, embedder, vectorStore, nil)
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

// --- TESTS RAG ---

// Test 4: Verificar que RAG inyecta contexto cuando encuentra documentos relevantes
func TestExecuteChat_RAGWithContext(t *testing.T) {
	svc, local, _, _, vectorStore := setupService()

	// Configurar el VectorStore para retornar documentos relevantes
	vectorStore.SearchResults = []domain.SearchResult{
		{
			Score: 0.85, // Score alto = relevante
			Document: domain.VectorDocument{
				ID:      "doc-1",
				Content: "Go es un lenguaje de programación creado por Google en 2009.",
				Metadata: map[string]interface{}{
					"filename": "golang-intro.txt",
				},
			},
		},
		{
			Score: 0.72,
			Document: domain.VectorDocument{
				ID:      "doc-2",
				Content: "Go es conocido por su simplicidad y eficiencia en concurrencia.",
				Metadata: map[string]interface{}{
					"filename": "golang-features.txt",
				},
			},
		},
	}

	// Variable para capturar los mensajes que recibe el provider
	var capturedMessages []domain.ChatMessage

	local.OnGenerateStream = func(req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
		capturedMessages = req.Messages

		// 1. Verificar que hay un system message al inicio
		if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
			t.Error("RAG debería inyectar un mensaje de sistema al inicio cuando hay contexto")
		}

		// 2. Verificar que el contenido del system message incluye el contexto
		systemContent := req.Messages[0].Content
		if !containsSubstring(systemContent, "golang-intro.txt") {
			t.Error("El system prompt debería incluir la fuente del documento")
		}
		if !containsSubstring(systemContent, "Go es un lenguaje de programación") {
			t.Error("El system prompt debería incluir el contenido del documento")
		}

		// 3. Verificar que el mensaje del usuario está presente
		userMsgFound := false
		for _, msg := range req.Messages {
			if msg.Role == "user" && msg.Content == "¿Qué es Go?" {
				userMsgFound = true
				break
			}
		}
		if !userMsgFound {
			t.Error("El mensaje del usuario debería estar presente en los mensajes enviados al provider")
		}

		return mockStreamResponse("Go es un lenguaje de Google", 1)
	}

	resChan := make(chan domain.ChatResponse)
	go func() {
		defer close(resChan)
		req := domain.ChatRequest{
			Messages: []domain.ChatMessage{{Role: "user", Content: "¿Qué es Go?"}},
		}
		cfg := domain.APIKeyConfig{AllowedProviders: []string{"ollama"}}
		_ = svc.ExecuteChat(context.Background(), req, cfg, resChan)
	}()

	// Consumir respuesta
	for range resChan {
	}

	// 4. Validación adicional: el primer mensaje debe ser system
	if len(capturedMessages) == 0 {
		t.Fatal("No se capturaron mensajes")
	}
	if capturedMessages[0].Role != "system" {
		t.Errorf("El primer mensaje debería ser 'system', es '%s'", capturedMessages[0].Role)
	}
}

// Test 5: Verificar que RAG NO inyecta contexto cuando NO encuentra documentos relevantes (scores bajos)
func TestExecuteChat_RAGWithoutContext(t *testing.T) {
	svc, local, _, _, vectorStore := setupService()

	// Configurar VectorStore con documentos de score bajo (< 0.25)
	vectorStore.SearchResults = []domain.SearchResult{
		{
			Score: 0.15, // Score bajo = no relevante
			Document: domain.VectorDocument{
				ID:      "doc-irrelevant",
				Content: "Contenido irrelevante",
			},
		},
	}

	var capturedMessages []domain.ChatMessage

	local.OnGenerateStream = func(req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
		capturedMessages = req.Messages

		// Verificar que NO hay system message (o si hay, no es de RAG)
		if len(req.Messages) > 0 && req.Messages[0].Role == "system" {
			systemContent := req.Messages[0].Content
			if containsSubstring(systemContent, "CONTEXTO") {
				t.Error("RAG NO debería inyectar contexto cuando los scores son bajos")
			}
		}

		return mockStreamResponse("No tengo información", 1)
	}

	resChan := make(chan domain.ChatResponse)
	go func() {
		defer close(resChan)
		req := domain.ChatRequest{
			Messages: []domain.ChatMessage{{Role: "user", Content: "¿Qué es XYZ?"}},
		}
		cfg := domain.APIKeyConfig{AllowedProviders: []string{"ollama"}}
		_ = svc.ExecuteChat(context.Background(), req, cfg, resChan)
	}()

	for range resChan {
	}

	// Validar que no hay system message de RAG
	if len(capturedMessages) > 0 && capturedMessages[0].Role == "system" {
		if containsSubstring(capturedMessages[0].Content, "--- CONTEXTO ---") {
			t.Error("No debería haber contexto RAG cuando los documentos tienen score bajo")
		}
	}
}

// Test 6: Verificar manejo de errores en RAG (embedder falla, pero el chat continúa)
func TestExecuteChat_RAGErrorHandling(t *testing.T) {
	// En este caso, creamos un setup custom con un embedder que falla
	local := &MockLLMProvider{Name: "ollama"}
	external := &MockLLMProvider{Name: "groq"}
	sessionRepo := NewMockSessionRepository()
	vectorStore := &MockVectorStore{SearchResults: []domain.SearchResult{}}

	// Mock de embedder que falla
	failingEmbedder := &MockFailingEmbedder{}

	// graphRepo nil para este test
	svc := NewChatService(local, external, sessionRepo, failingEmbedder, vectorStore, nil)

	local.OnGenerateStream = func(req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
		// El chat debería continuar normalmente aunque el RAG falle
		return mockStreamResponse("Respuesta sin contexto", 1)
	}

	resChan := make(chan domain.ChatResponse)
	var chatError error

	go func() {
		defer close(resChan)
		req := domain.ChatRequest{
			Messages: []domain.ChatMessage{{Role: "user", Content: "Hola"}},
		}
		cfg := domain.APIKeyConfig{AllowedProviders: []string{"ollama"}}
		chatError = svc.ExecuteChat(context.Background(), req, cfg, resChan)
	}()

	// Consumir respuesta
	received := ""
	for res := range resChan {
		received += res.Content
	}

	// El chat debe completarse exitosamente aunque RAG falle
	if chatError != nil {
		t.Errorf("El chat no debería fallar cuando RAG tiene error: %v", chatError)
	}
	if received != "Respuesta sin contexto" {
		t.Errorf("Debería recibir respuesta normal, recibió: '%s'", received)
	}
}

// --- HELPER MOCKS ADICIONALES ---

// Mock de embedder que siempre falla (para test de error handling)
type MockFailingEmbedder struct{}

func (m *MockFailingEmbedder) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	return nil, fmt.Errorf("error simulado de embedder")
}

// Helper para buscar substring (case-insensitive sería mejor, pero esto es suficiente)
func containsSubstring(text, substr string) bool {
	return len(text) >= len(substr) && (text == substr || len(text) > len(substr) && stringContains(text, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
