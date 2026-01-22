package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
)

const (
	CollectionName = "knowledge_base"
	VectorSize     = 768
	ChunkSize      = 500

	// PROMPT PARA EXTRAER GRAFO
	GraphSystemPrompt = `
Eres un experto analizando arquitectura de software. Tu trabajo es construir un Grafo de Conocimiento.
Analiza el texto y extrae Nodos y Relaciones.

NODOS (Entities):
- Identifica tecnologías, herramientas, conceptos, patrones, personas u organizaciones.
- Tipos sugeridos: TOOL, CONCEPT, PERSON, ORG, PROJECT.

RELACIONES (Edges):
- Identifica cómo interactúan (ej: USES, DEPLOYS, CREATED_BY, IS_A).
- Usa verbos en INFINITIVO, MAYÚSCULAS y en INGLÉS (ej: USES, not "usa").

OUTPUT JSON STRICTO:
{
  "nodes": [{"id": "Neo4j", "label": "TOOL"}, {"id": "Database", "label": "CONCEPT"}],
  "relationships": [{"source": "Neo4j", "target": "Database", "type": "IS_A"}]
}
`
)

type IngestService struct {
	jobRepo      ports.JobRepository
	embedder     ports.EmbeddingGenerator
	vectorStore  ports.VectorStore
	extractorLLM ports.LLMProvider
	graphStore   ports.GraphRepository
}

// Constructor actualizado
func NewIngestService(jobRepo ports.JobRepository, embedder ports.EmbeddingGenerator, vectorStore ports.VectorStore, extractorLLM ports.LLMProvider, graphStore ports.GraphRepository) *IngestService {
	return &IngestService{
		jobRepo:      jobRepo,
		embedder:     embedder,
		vectorStore:  vectorStore,
		extractorLLM: extractorLLM,
		graphStore:   graphStore,
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

	// Ejecutar en goroutine (Async Worker)
	go s.processJob(context.Background(), job, req.Content)

	return &job, nil
}

func (s *IngestService) GetJobStatus(ctx context.Context, jobID string) (*domain.IngestJob, error) {
	return s.jobRepo.GetJob(ctx, jobID)
}

// --- LÓGICA DEL WORKER ---

func (s *IngestService) processJob(ctx context.Context, job domain.IngestJob, content string) {
	// 1. Validar/Crear colección Qdrant
	if err := s.vectorStore.EnsureCollection(ctx, CollectionName, VectorSize); err != nil {
		s.failJob(ctx, job, "Error iniciando vector store: "+err.Error())
		return
	}

	// 2. Split Texto
	chunks := s.splitText(content, ChunkSize)
	log.Printf("[Job %s] Texto dividido en %d chunks", job.ID, len(chunks))

	var vectorsToUpsert []domain.VectorDocument

	// Acumuladores para estadísticas de Grafo
	nodesCount := 0
	relsCount := 0

	for i, chunkText := range chunks {
		// A. VECTOR RAG (Existente)
		embedding, err := s.embedder.GenerateEmbedding(ctx, chunkText)
		if err != nil {
			s.failJob(ctx, job, fmt.Sprintf("Error generando embedding chunk %d: %v", i, err))
			return
		}

		docID := fmt.Sprintf("%s-chunk-%d", job.ID, i)
		chunkMeta := make(map[string]interface{})
		for k, v := range job.Metadata {
			chunkMeta[k] = v
		}
		chunkMeta["chunk_index"] = i
		chunkMeta["source_text"] = chunkText

		vectorsToUpsert = append(vectorsToUpsert, domain.VectorDocument{
			ID:       docID,
			Content:  chunkText,
			Vector:   embedding,
			Metadata: chunkMeta,
		})

		// B. GRAPH RAG (Nuevo - Solo procesamos chunks si tenemos GraphStore)
		if s.graphStore != nil {
			log.Printf("[Job %s] Extrayendo grafo del chunk %d...", job.ID, i)
			n, r, err := s.extractAndSaveGraph(ctx, chunkText)
			if err != nil {
				// No fallamos todo el job si el grafo falla, solo logueamos (Soft Fail)
				log.Printf("⚠️ Error GraphRAG en chunk %d: %v", i, err)
			} else {
				nodesCount += n
				relsCount += r
			}
		}
	}

	// 3. Guardar Vectores en Qdrant
	if len(vectorsToUpsert) > 0 {
		if err := s.vectorStore.Upsert(ctx, CollectionName, vectorsToUpsert); err != nil {
			s.failJob(ctx, job, "Error guardando en Qdrant: "+err.Error())
			return
		}
	}

	// 4. Finalizar
	job.Status = domain.StatusCompleted
	job.Message = fmt.Sprintf("Completado: %d vectores, %d nodos, %d relaciones guardadas.", len(vectorsToUpsert), nodesCount, relsCount)
	_ = s.jobRepo.SaveJob(ctx, job)
	log.Printf("[Job %s] %s", job.ID, job.Message)
}

func (s *IngestService) failJob(ctx context.Context, job domain.IngestJob, msg string) {
	log.Printf("[Job %s] Falló: %s", job.ID, msg)
	job.Status = domain.StatusFailed
	job.Message = msg
	_ = s.jobRepo.SaveJob(ctx, job)
}

// --- LÓGICA DE EXTRACCIÓN Y GUARDADO DE GRAFO ---

type graphJSON struct {
	Nodes []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	} `json:"nodes"`
	Relationships []struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Type   string `json:"type"`
	} `json:"relationships"`
}

func (s *IngestService) extractAndSaveGraph(ctx context.Context, text string) (int, int, error) {
	// 1. Llamar al LLM para extraer JSON
	req := domain.ChatRequest{
		Messages: []domain.ChatMessage{
			{Role: "system", Content: GraphSystemPrompt},
			{Role: "user", Content: fmt.Sprintf("Texto a analizar: \n%s", text)},
		},
		PreferredProvider: "groq", // Forzamos Groq si es posible por velocidad
	}

	// Usamos un canal para recibir la respuesta (tu interfaz es streaming)
	resChan, errChan := s.extractorLLM.GenerateStream(ctx, req)

	var fullResponse strings.Builder
	for {
		select {
		case chunk, ok := <-resChan:
			if !ok {
				goto DoneReading
			}
			fullResponse.WriteString(chunk.Content)
		case err := <-errChan:
			return 0, 0, err
		case <-ctx.Done():
			return 0, 0, ctx.Err()
		}
	}
DoneReading:

	jsonStr := fullResponse.String()
	// Limpieza básica por si el LLM mete markdown ```json ... ```
	jsonStr = cleanJSON(jsonStr)

	// 2. Parsear JSON
	var data graphJSON
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return 0, 0, fmt.Errorf("error parseando JSON del LLM: %v | Raw: %s", err, jsonStr)
	}

	// 3. Escribir en Neo4j (Cypher)
	// Guardamos Nodos
	for _, n := range data.Nodes {
		// Normalizamos ID a minúsculas para evitar duplicados por casing, pero guardamos el original como nombre
		cleanID := strings.ToLower(strings.TrimSpace(n.ID))
		label := sanitize(n.Label) // Evitar inyecciones
		if label == "" {
			label = "Concept"
		}

		query := fmt.Sprintf("MERGE (n:Entity {id: $id}) SET n.name = $name, n:%s", label)
		params := map[string]interface{}{
			"id":   cleanID,
			"name": n.ID,
		}
		if err := s.graphStore.ExecuteWrite(ctx, query, params); err != nil {
			log.Printf("Error escribiendo nodo %s: %v", n.ID, err)
		}
	}

	// Guardamos Relaciones
	for _, r := range data.Relationships {
		sourceID := strings.ToLower(strings.TrimSpace(r.Source))
		targetID := strings.ToLower(strings.TrimSpace(r.Target))
		relType := sanitize(r.Type)
		if relType == "" {
			relType = "RELATED_TO"
		}

		// Query: Busca A y B, crea relación si no existe
		query := fmt.Sprintf(`
			MATCH (a:Entity {id: $source}), (b:Entity {id: $target})
			MERGE (a)-[r:%s]->(b)
		`, relType)

		params := map[string]interface{}{
			"source": sourceID,
			"target": targetID,
		}
		if err := s.graphStore.ExecuteWrite(ctx, query, params); err != nil {
			log.Printf("Error escribiendo relación %s->%s: %v", r.Source, r.Target, err)
		}
	}

	return len(data.Nodes), len(data.Relationships), nil
}

// splitText divide el texto en chunks (Implementación simple)
func (s *IngestService) splitText(text string, chunkSize int) []string {
	var chunks []string
	runes := []rune(text)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		// En una impementación real, aquí buscaríamos el último espacio para no cortar palabras
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

func cleanJSON(s string) string {
	s = strings.ReplaceAll(s, "```json", "")
	s = strings.ReplaceAll(s, "```", "")
	return strings.TrimSpace(s)
}

// sanitize limpia strings para usarlos en Cypher (evita inyecciones básicas)
func sanitize(s string) string {
	reg, _ := regexp.Compile("[^a-zA-Z0-9_]+")
	return strings.ToUpper(reg.ReplaceAllString(s, "_"))
}
