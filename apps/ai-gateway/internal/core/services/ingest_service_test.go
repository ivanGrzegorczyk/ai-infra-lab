package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
)

// --- MOCKS ESPECÍFICOS PARA INGEST ---

// 1. Mock Job Repository (Thread Safe porque processJob corre en paralelo)
type MockJobRepo struct {
	mu   sync.Mutex
	jobs map[string]domain.IngestJob
}

func NewMockJobRepo() *MockJobRepo {
	return &MockJobRepo{jobs: make(map[string]domain.IngestJob)}
}

func (m *MockJobRepo) SaveJob(ctx context.Context, job domain.IngestJob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.jobs[job.ID] = job
	return nil
}

func (m *MockJobRepo) GetJob(ctx context.Context, jobID string) (*domain.IngestJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return nil, nil
	}
	return &j, nil
}

// 2. Mock Embedder
type MockEmbedder struct {
	Fail bool
}

func (m *MockEmbedder) GenerateEmbedding(ctx context.Context, text string) ([]float32, error) {
	if m.Fail {
		return nil, errors.New("embedding error simulado")
	}
	// Retornamos un vector dummy de longitud 3
	return []float32{0.1, 0.2, 0.3}, nil
}

// 3. Mock Vector Store (para Ingest)
type MockVectorStoreForIngest struct {
	UpsertedDocs []domain.VectorDocument
	Fail         bool
}

func (m *MockVectorStoreForIngest) EnsureCollection(ctx context.Context, name string, size uint64) error {
	return nil
}

func (m *MockVectorStoreForIngest) Upsert(ctx context.Context, collectionName string, docs []domain.VectorDocument) error {
	if m.Fail {
		return errors.New("qdrant error simulado")
	}
	m.UpsertedDocs = append(m.UpsertedDocs, docs...)
	return nil
}
func (m *MockVectorStoreForIngest) Search(ctx context.Context, collectionName string, vector []float32, limit uint64) ([]domain.SearchResult, error) {
	return nil, nil
}

// 4. Mock LLM Provider (para GraphRAG extractor)
type MockExtractorLLM struct {
	Response string // JSON que retornará el mock
	Fail     bool
}

func (m *MockExtractorLLM) GenerateStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
	resChan := make(chan domain.ChatResponse, 1)
	errChan := make(chan error, 1)

	go func() {
		if m.Fail {
			errChan <- errors.New("LLM error simulado")
			close(errChan)
			close(resChan)
			return
		}
		resChan <- domain.ChatResponse{Content: m.Response}
		close(resChan)
		// No cerramos errChan inmediatamente para evitar race condition en el select
		// El servicio sale del loop cuando resChan se cierra
	}()

	return resChan, errChan
}

func (m *MockExtractorLLM) GetName() string {
	return "mock-extractor"
}

// 5. Mock Graph Store (para Neo4j)
type MockGraphStore struct {
	mu           sync.Mutex
	WrittenNodes []map[string]interface{}
	WrittenRels  []map[string]interface{}
	Fail         bool
}

func NewMockGraphStore() *MockGraphStore {
	return &MockGraphStore{
		WrittenNodes: []map[string]interface{}{},
		WrittenRels:  []map[string]interface{}{},
	}
}

func (m *MockGraphStore) Close() error {
	return nil
}

func (m *MockGraphStore) ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error) {
	return nil, nil
}

func (m *MockGraphStore) ExecuteWrite(ctx context.Context, query string, params map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.Fail {
		return errors.New("neo4j error simulado")
	}

	// Detectar si es nodo o relación por el contenido del query
	if _, hasSource := params["source"]; hasSource {
		m.WrittenRels = append(m.WrittenRels, params)
	} else if _, hasID := params["id"]; hasID {
		m.WrittenNodes = append(m.WrittenNodes, params)
	}
	return nil
}

func (m *MockGraphStore) HealthCheck(ctx context.Context) error {
	return nil
}

// --- TEST SETUP ---

func setupIngest() (*IngestService, *MockJobRepo, *MockEmbedder, *MockVectorStoreForIngest) {
	jobRepo := NewMockJobRepo()
	embedder := &MockEmbedder{}
	vectorStore := &MockVectorStoreForIngest{}

	// extractorLLM y graphStore se pasan como nil para tests básicos sin GraphRAG
	// El servicio verifica nil antes de usar graphStore
	svc := NewIngestService(jobRepo, embedder, vectorStore, nil, nil)
	return svc, jobRepo, embedder, vectorStore
}

// Setup con GraphRAG habilitado
func setupIngestWithGraphRAG() (*IngestService, *MockJobRepo, *MockEmbedder, *MockVectorStoreForIngest, *MockExtractorLLM, *MockGraphStore) {
	jobRepo := NewMockJobRepo()
	embedder := &MockEmbedder{}
	vectorStore := &MockVectorStoreForIngest{}
	extractorLLM := &MockExtractorLLM{}
	graphStore := NewMockGraphStore()

	svc := NewIngestService(jobRepo, embedder, vectorStore, extractorLLM, graphStore)
	return svc, jobRepo, embedder, vectorStore, extractorLLM, graphStore
}

// --- TESTS ---

// Test 1: Verificar que StartIngestion crea el Job y retorna rápido
func TestStartIngestion_CreatesJob(t *testing.T) {
	svc, repo, _, _ := setupIngest()

	req := domain.IngestRequest{
		Content:  "Texto de prueba",
		Metadata: map[string]interface{}{"author": "Ivan"},
	}

	job, err := svc.StartIngestion(context.Background(), req)
	if err != nil {
		t.Fatalf("Error iniciando ingesta: %v", err)
	}

	if job.ID == "" {
		t.Error("El Job ID no debería estar vacío")
	}
	if job.Status != domain.StatusPending {
		t.Errorf("Estado inicial debería ser pending, es %s", job.Status)
	}

	// Verificar persistencia inicial
	savedJob, _ := repo.GetJob(context.Background(), job.ID)
	if savedJob == nil {
		t.Error("El job no se guardó en el repositorio")
	}
}

// Test 2: Verificar la lógica de negocio completa (Chunking + Embedding + Guardado)
// Llamamos a processJob directamente para no lidiar con race conditions del async
func TestProcessJob_SuccessFlow(t *testing.T) {
	svc, repo, _, vectorStore := setupIngest()

	// Creamos un Job manual
	job := domain.IngestJob{
		ID:        "job-sync-test",
		Status:    domain.StatusPending,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{"filename": "doc.txt"},
	}
	// Lo guardamos en el repo mock para que processJob pueda actualizarlo
	repo.SaveJob(context.Background(), job)

	// Simulamos un texto largo para probar el chunking
	// Asumiendo ChunkSize=500 en tu servicio. Creamos un texto de ~600 chars
	longText := ""
	for i := 0; i < 60; i++ {
		longText += "0123456789" // 10 chars * 60 = 600 chars
	}

	// EJECUCIÓN SÍNCRONA
	svc.processJob(context.Background(), job, longText)

	// VALIDACIONES

	// 1. Estado Final del Job
	finalJob, _ := repo.GetJob(context.Background(), job.ID)
	if finalJob.Status != domain.StatusCompleted {
		t.Errorf("El job debería estar completed, está en: %s (Msg: %s)", finalJob.Status, finalJob.Message)
	}

	// 2. Chunking Logic
	// Con 600 chars y chunk size 500, deberíamos tener 2 vectores upserted
	if len(vectorStore.UpsertedDocs) != 2 {
		t.Errorf("Se esperaban 2 chunks/documentos, se generaron %d", len(vectorStore.UpsertedDocs))
	}

	// 3. Metadata Propagation
	doc1 := vectorStore.UpsertedDocs[0]
	if val, ok := doc1.Metadata["filename"]; !ok || val != "doc.txt" {
		t.Error("Se perdió la metadata original del job en el vector")
	}
	if _, ok := doc1.Metadata["chunk_index"]; !ok {
		t.Error("No se agregó chunk_index a la metadata")
	}
}

// Test 3: Verificar manejo de errores (Embedding falla)
func TestProcessJob_EmbeddingFailure(t *testing.T) {
	svc, repo, embedder, _ := setupIngest()

	// Configuramos el mock para fallar
	embedder.Fail = true

	job := domain.IngestJob{ID: "job-fail", Status: domain.StatusPending}
	repo.SaveJob(context.Background(), job)

	svc.processJob(context.Background(), job, "Texto simple")

	finalJob, _ := repo.GetJob(context.Background(), job.ID)
	if finalJob.Status != domain.StatusFailed {
		t.Errorf("El job debería haber fallado, está en %s", finalJob.Status)
	}
}

// --- TESTS GRAPHRAG ---

// Test 4: Verificar que GraphRAG extrae nodos y relaciones correctamente
func TestProcessJob_GraphRAG_ExtractsNodesAndRelations(t *testing.T) {
	svc, repo, _, vectorStore, extractorLLM, graphStore := setupIngestWithGraphRAG()

	// Configuramos el mock del LLM para retornar un JSON válido de grafo
	extractorLLM.Response = `{"nodes": [{"id": "Go", "label": "TOOL"}, {"id": "Microservices", "label": "CONCEPT"}], "relationships": [{"source": "Go", "target": "Microservices", "type": "BUILDS"}]}`

	job := domain.IngestJob{
		ID:        "job-graphrag-test",
		Status:    domain.StatusPending,
		CreatedAt: time.Now(),
		Metadata:  map[string]interface{}{"source": "test"},
	}
	repo.SaveJob(context.Background(), job)

	// Texto suficientemente largo para superar MinChunkForGraphRAG (50 chars)
	svc.processJob(context.Background(), job, "Go es un lenguaje de programación ideal para construir microservices escalables")

	// VALIDACIONES

	// 1. Job completado
	finalJob, _ := repo.GetJob(context.Background(), job.ID)
	if finalJob.Status != domain.StatusCompleted {
		t.Errorf("El job debería estar completed, está en: %s (Msg: %s)", finalJob.Status, finalJob.Message)
	}

	// 2. Vectores guardados (RAG tradicional sigue funcionando)
	if len(vectorStore.UpsertedDocs) != 1 {
		t.Errorf("Se esperaba 1 vector, se generaron %d", len(vectorStore.UpsertedDocs))
	}

	// 3. Nodos escritos en Neo4j
	if len(graphStore.WrittenNodes) != 2 {
		t.Errorf("Se esperaban 2 nodos escritos, se escribieron %d", len(graphStore.WrittenNodes))
	}

	// 4. Relaciones escritas en Neo4j
	if len(graphStore.WrittenRels) != 1 {
		t.Errorf("Se esperaba 1 relación escrita, se escribieron %d", len(graphStore.WrittenRels))
	}

	// 5. Verificar contenido del mensaje final (debe incluir stats de grafo)
	if finalJob.Message == "" {
		t.Error("El mensaje final no debería estar vacío")
	}
}

// Test 5: Verificar que GraphRAG falla gracefully (soft fail - no rompe el job)
func TestProcessJob_GraphRAG_SoftFailOnLLMError(t *testing.T) {
	svc, repo, _, vectorStore, extractorLLM, _ := setupIngestWithGraphRAG()

	// Configuramos el LLM para fallar
	extractorLLM.Fail = true

	job := domain.IngestJob{
		ID:        "job-graphrag-softfail",
		Status:    domain.StatusPending,
		CreatedAt: time.Now(),
	}
	repo.SaveJob(context.Background(), job)

	// Texto suficientemente largo para pasar el filtro MinChunkForGraphRAG
	svc.processJob(context.Background(), job, "Este es un texto de prueba lo suficientemente largo para procesarlo")

	// El job debe completarse aunque GraphRAG falle (soft fail)
	finalJob, _ := repo.GetJob(context.Background(), job.ID)
	if finalJob.Status != domain.StatusCompleted {
		t.Errorf("El job debería completarse aunque GraphRAG falle, está en: %s", finalJob.Status)
	}

	// Los vectores deben haberse guardado correctamente
	if len(vectorStore.UpsertedDocs) != 1 {
		t.Errorf("Los vectores deberían guardarse aunque GraphRAG falle, hay %d", len(vectorStore.UpsertedDocs))
	}
}

// Test 6: Verificar que GraphRAG maneja JSON inválido del LLM
func TestProcessJob_GraphRAG_InvalidJSONFromLLM(t *testing.T) {
	svc, repo, _, vectorStore, extractorLLM, graphStore := setupIngestWithGraphRAG()

	// El LLM retorna JSON inválido
	extractorLLM.Response = `esto no es json válido {}`

	job := domain.IngestJob{
		ID:        "job-graphrag-badjson",
		Status:    domain.StatusPending,
		CreatedAt: time.Now(),
	}
	repo.SaveJob(context.Background(), job)

	// Texto suficientemente largo para pasar el filtro MinChunkForGraphRAG
	svc.processJob(context.Background(), job, "Este es un texto de prueba lo suficientemente largo para procesarlo")

	// El job debe completarse (soft fail en GraphRAG)
	finalJob, _ := repo.GetJob(context.Background(), job.ID)
	if finalJob.Status != domain.StatusCompleted {
		t.Errorf("El job debería completarse con JSON inválido, está en: %s", finalJob.Status)
	}

	// No debe haber nodos escritos porque el JSON era inválido
	if len(graphStore.WrittenNodes) != 0 {
		t.Errorf("No deberían escribirse nodos con JSON inválido, hay %d", len(graphStore.WrittenNodes))
	}

	// Pero los vectores sí deben guardarse
	if len(vectorStore.UpsertedDocs) != 1 {
		t.Errorf("Los vectores deberían guardarse aunque el JSON sea inválido")
	}
}

// Test 7: Verificar GraphRAG con múltiples chunks
func TestProcessJob_GraphRAG_MultipleChunks(t *testing.T) {
	svc, repo, _, vectorStore, extractorLLM, graphStore := setupIngestWithGraphRAG()

	// Cada chunk extraerá 1 nodo
	extractorLLM.Response = `{
		"nodes": [{"id": "Entity", "label": "CONCEPT"}],
		"relationships": []
	}`

	job := domain.IngestJob{
		ID:        "job-graphrag-multichunk",
		Status:    domain.StatusPending,
		CreatedAt: time.Now(),
	}
	repo.SaveJob(context.Background(), job)

	// Texto largo para generar 2 chunks (ChunkSize=500)
	longText := ""
	for i := 0; i < 60; i++ {
		longText += "0123456789" // 600 chars = 2 chunks
	}

	svc.processJob(context.Background(), job, longText)

	finalJob, _ := repo.GetJob(context.Background(), job.ID)
	if finalJob.Status != domain.StatusCompleted {
		t.Errorf("El job debería estar completed, está en: %s", finalJob.Status)
	}

	// 2 chunks = 2 vectores
	if len(vectorStore.UpsertedDocs) != 2 {
		t.Errorf("Se esperaban 2 vectores, hay %d", len(vectorStore.UpsertedDocs))
	}

	// 2 chunks = 2 llamadas al LLM = 2 nodos (uno por chunk)
	if len(graphStore.WrittenNodes) != 2 {
		t.Errorf("Se esperaban 2 nodos (1 por chunk), hay %d", len(graphStore.WrittenNodes))
	}
}
