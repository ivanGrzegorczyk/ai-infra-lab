package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
)

const (
	CollectionName = "knowledge_base"
	VectorSize     = 768 // Tamaño estándar de nomic-embed-text
	ChunkSize      = 500 // Caracteres aprox por chunk
	ChunkOverlap   = 50  // Solapamiento para no cortar frases
)

type IngestService struct {
	jobRepo     ports.JobRepository
	embedder    ports.EmbeddingGenerator
	vectorStore ports.VectorStore
}

func NewIngestService(jobRepo ports.JobRepository, embedder ports.EmbeddingGenerator, vectorStore ports.VectorStore) *IngestService {
	return &IngestService{
		jobRepo:     jobRepo,
		embedder:    embedder,
		vectorStore: vectorStore,
	}
}

func (s *IngestService) StartIngestion(ctx context.Context, req domain.IngestRequest) (*domain.IngestJob, error) {
	jobID := uuid.New().String()
	job := domain.IngestJob{
		ID:        jobID,
		Status:    domain.StatusPending,
		CreatedAt: time.Now(),
		Metadata:  req.Metadata,
	}

	if err := s.jobRepo.SaveJob(ctx, job); err != nil {
		return nil, err
	}

	// Disparar worker en background
	go s.processJob(context.Background(), job, req.Content)

	return &job, nil
}

func (s *IngestService) GetJobStatus(ctx context.Context, jobID string) (*domain.IngestJob, error) {
	return s.jobRepo.GetJob(ctx, jobID)
}

// processJob La lógica principal de procesamiento del job
func (s *IngestService) processJob(ctx context.Context, job domain.IngestJob, content string) {
	log.Printf("[Job %s] Iniciando procesamiento...", job.ID)

	// 1. Actualizar estado a Processing
	job.Status = domain.StatusProcessing
	_ = s.jobRepo.SaveJob(ctx, job)

	// 2. Asegurar que la colección existe en Qdrant
	if err := s.vectorStore.EnsureCollection(ctx, CollectionName, VectorSize); err != nil {
		s.failJob(ctx, job, "Error conectando con Vector DB: "+err.Error())
		return
	}

	// 3. Chunking (Dividir texto)
	chunks := s.splitText(content, ChunkSize, ChunkOverlap)
	log.Printf("[Job %s] Texto dividido en %d chunks", job.ID, len(chunks))

	var vectorsToUpsert []domain.VectorDocument

	// 4. Vectorización (Loop por cada chunk)
	for i, chunkText := range chunks {
		embedding, err := s.embedder.GenerateEmbedding(ctx, chunkText)
		if err != nil {
			s.failJob(ctx, job, fmt.Sprintf("Error generando embedding chunk %d: %v", i, err))
			return
		}

		// Preparar documento
		docID := uuid.New().String()

		// Clonar metadata del job y agregar info del chunk
		chunkMeta := make(map[string]interface{})
		for k, v := range job.Metadata {
			chunkMeta[k] = v
		}
		chunkMeta["chunk_index"] = i
		chunkMeta["job_id"] = job.ID
		chunkMeta["source_text"] = chunkText // Guarda el texto original para recuperarlo después

		vectorsToUpsert = append(vectorsToUpsert, domain.VectorDocument{
			ID:       docID,
			Content:  chunkText,
			Vector:   embedding,
			Metadata: chunkMeta,
		})
	}

	// 5. Guardar en Qdrant (Batch upsert)
	if len(vectorsToUpsert) > 0 {
		if err := s.vectorStore.Upsert(ctx, CollectionName, vectorsToUpsert); err != nil {
			s.failJob(ctx, job, "Error guardando en Qdrant: "+err.Error())
			return
		}
	}

	// 6. Éxito
	job.Status = domain.StatusCompleted
	job.Message = fmt.Sprintf("Procesados %d chunks exitosamente", len(vectorsToUpsert))
	_ = s.jobRepo.SaveJob(ctx, job)
	log.Printf("[Job %s] Completado con éxito", job.ID)
}

func (s *IngestService) failJob(ctx context.Context, job domain.IngestJob, msg string) {
	log.Printf("[Job %s] Falló: %s", job.ID, msg)
	job.Status = domain.StatusFailed
	job.Message = msg
	_ = s.jobRepo.SaveJob(ctx, job)
}

// splitText Algoritmo simple de ventana deslizante
func (s *IngestService) splitText(text string, size int, overlap int) []string {
	if len(text) <= size {
		return []string{text}
	}
	var chunks []string
	runes := []rune(text) // Usar runes para no romper caracteres UTF-8

	for i := 0; i < len(runes); i += (size - overlap) {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}
