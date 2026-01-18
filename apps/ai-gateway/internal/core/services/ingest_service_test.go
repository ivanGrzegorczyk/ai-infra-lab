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

// --- TEST SETUP ---

func setupIngest() (*IngestService, *MockJobRepo, *MockEmbedder, *MockVectorStoreForIngest) {
	jobRepo := NewMockJobRepo()
	embedder := &MockEmbedder{}
	vectorStore := &MockVectorStoreForIngest{}

	svc := NewIngestService(jobRepo, embedder, vectorStore)
	return svc, jobRepo, embedder, vectorStore
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
