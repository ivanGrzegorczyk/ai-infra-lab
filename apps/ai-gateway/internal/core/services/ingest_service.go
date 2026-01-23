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
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/observability"
)

const (
	CollectionName      = "knowledge_base"
	VectorSize          = 768
	ChunkSize           = 500
	MinChunkForGraphRAG = 50 // Mínimo de caracteres para intentar GraphRAG

	// PROMPT PARA EXTRAER GRAFO - Mejorado para mayor calidad
	GraphSystemPrompt = `You are a knowledge graph extraction expert. Extract entities and relationships from the text.

RULES:
1. Output ONLY valid JSON, no explanations or markdown
2. Entity IDs must be lowercase, alphanumeric with underscores only (e.g., "neo4j", "api_gateway")
3. Labels: TOOL, CONCEPT, PERSON, ORG, PROJECT (pick the most specific)
4. Relationship types: UPPERCASE English verbs (USES, IMPLEMENTS, DEPLOYS, CONNECTS_TO, IS_A, CREATED_BY)
5. Only extract meaningful technical entities, skip generic words like "system", "data", "file"
6. Maximum 10 nodes and 15 relationships per chunk

OUTPUT FORMAT (strict JSON):
{"nodes":[{"id":"redis","label":"TOOL"}],"relationships":[{"source":"api_gateway","target":"redis","type":"USES"}]}
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
	totalChunks := len(chunks)
	graphSkipped := 0

	for i, chunkText := range chunks {
		// Progreso: Mostrar porcentaje cada 10 chunks o en el primero/último
		if i == 0 || (i+1) == totalChunks || (i+1)%10 == 0 {
			progress := float64(i+1) / float64(totalChunks) * 100
			log.Printf("[Job %s] Progreso: %d/%d (%.1f%%)", job.ID, i+1, totalChunks, progress)
		}
		// A. VECTOR RAG (Existente)
		embedding, err := s.embedder.GenerateEmbedding(ctx, chunkText)
		if err != nil {
			s.failJob(ctx, job, fmt.Sprintf("Error generando embedding chunk %d: %v", i, err))
			return
		}

		// Genera un UUID válido para cada chunk (Qdrant requiere UUIDs)
		docID := uuid.New().String()
		chunkMeta := make(map[string]interface{})
		for k, v := range job.Metadata {
			chunkMeta[k] = v
		}
		chunkMeta["job_id"] = job.ID
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
			// Saltar chunks muy cortos para evitar errores del LLM
			if len(strings.TrimSpace(chunkText)) < MinChunkForGraphRAG {
				graphSkipped++
				continue
			}
			n, r, err := s.extractAndSaveGraph(ctx, chunkText)
			if err != nil {
				// No falla todo el job si el grafo falla, solo loguea (Soft Fail)
				log.Printf("[Job %s] GraphRAG chunk %d: error - %v", job.ID, i, err)
			} else if n > 0 || r > 0 {
				nodesCount += n
				relsCount += r
				log.Printf("[Job %s] GraphRAG chunk %d: +%d nodos, +%d rels", job.ID, i, n, r)
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
	job.Message = fmt.Sprintf("Completado: %d vectores, %d nodos, %d rels (skipped %d chunks cortos)", len(vectorsToUpsert), nodesCount, relsCount, graphSkipped)
	_ = s.jobRepo.SaveJob(ctx, job)
	log.Printf("[Job %s] %s", job.ID, job.Message)

	// Registrar métricas
	observability.IngestJobsTotal.WithLabelValues("completed").Inc()
	observability.GraphRAGNodesExtracted.Add(float64(nodesCount))
	observability.GraphRAGRelationsExtracted.Add(float64(relsCount))
}

func (s *IngestService) failJob(ctx context.Context, job domain.IngestJob, msg string) {
	log.Printf("[Job %s] Falló: %s", job.ID, msg)
	job.Status = domain.StatusFailed
	job.Message = msg
	_ = s.jobRepo.SaveJob(ctx, job)

	// Registrar métrica de job fallido
	observability.IngestJobsTotal.WithLabelValues("failed").Inc()
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
		PreferredProvider: "groq",
	}

	// Usamos un canal para recibir la respuesta (tu interfaz es streaming)
	resChan, errChan := s.extractorLLM.GenerateStream(ctx, req)

	var fullResponse strings.Builder
	for {
		select {
		case chunk, ok := <-resChan:
			if !ok {
				log.Printf("  Canal cerrado, respuesta completa")
				goto DoneReading
			}
			fullResponse.WriteString(chunk.Content)
		case err, ok := <-errChan:
			if ok && err != nil {
				log.Printf("  Error del LLM: %v", err)
				return 0, 0, err
			}
			// Canal cerrado sin error, continuamos
		case <-ctx.Done():
			return 0, 0, ctx.Err()
		}
	}
DoneReading:

	jsonStr := strings.TrimSpace(fullResponse.String())

	// Si la respuesta está vacía, retornamos sin error (chunk sin entidades extraíbles)
	if jsonStr == "" {
		return 0, 0, nil
	}

	// Limpieza básica por si el LLM mete markdown ```json ... ```
	jsonStr = cleanJSON(jsonStr)

	// Validar que tengamos algo parecido a JSON
	if !strings.HasPrefix(jsonStr, "{") || !strings.HasSuffix(jsonStr, "}") {
		return 0, 0, nil // Respuesta inválida, ignorar silenciosamente
	}

	// 2. Parsear JSON
	var data graphJSON
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		// Solo logueamos si es un JSON que parece válido pero falló el parse
		return 0, 0, fmt.Errorf("JSON parse error: %v", err)
	}

	// Si no hay nodos ni relaciones, retornamos sin procesar
	if len(data.Nodes) == 0 && len(data.Relationships) == 0 {
		return 0, 0, nil
	}

	// 3. Escribir en Neo4j (Cypher)
	nodesWritten := 0
	relsWritten := 0

	// Guardamos Nodos
	for _, n := range data.Nodes {
		cleanID := normalizeID(n.ID)
		if cleanID == "" {
			continue
		}

		label := sanitize(n.Label)
		if label == "" {
			label = "CONCEPT"
		}

		keywords := extractKeywords(n.ID)
		query := fmt.Sprintf("MERGE (n:Entity {id: $id}) SET n.name = $name, n.keywords = $keywords, n.label = $label SET n:%s", label)
		params := map[string]interface{}{
			"id":       cleanID,
			"name":     n.ID,
			"keywords": keywords,
			"label":    label,
		}
		if err := s.graphStore.ExecuteWrite(ctx, query, params); err != nil {
			log.Printf("Error nodo %s: %v", cleanID, err)
		} else {
			nodesWritten++
		}
	}

	// Guardamos Relaciones
	for _, r := range data.Relationships {
		sourceID := normalizeID(r.Source)
		targetID := normalizeID(r.Target)
		if sourceID == "" || targetID == "" {
			continue
		}

		relType := sanitize(r.Type)
		if relType == "" {
			relType = "RELATED_TO"
		}

		query := fmt.Sprintf(`
			MATCH (a:Entity {id: $source}), (b:Entity {id: $target})
			MERGE (a)-[r:%s]->(b)
		`, relType)
		params := map[string]interface{}{
			"source": sourceID,
			"target": targetID,
		}
		if err := s.graphStore.ExecuteWrite(ctx, query, params); err != nil {
			log.Printf("Error rel %s->%s: %v", sourceID, targetID, err)
		} else {
			relsWritten++
		}
	}

	return nodesWritten, relsWritten, nil
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
	// Primero limpiamos markdown code blocks
	s = strings.ReplaceAll(s, "```json", "")
	s = strings.ReplaceAll(s, "```", "")
	s = strings.TrimSpace(s)

	// Si el LLM añadio texto antes del JSON, extraemos solo el JSON
	// Buscamos el primer '{' y el ultimo '}'
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start != -1 && end != -1 && end > start {
		s = s[start : end+1]
	}
	return s
}

// sanitize limpia strings para usarlos en Cypher (evita inyecciones basicas)
func sanitize(s string) string {
	reg, _ := regexp.Compile("[^a-zA-Z0-9_]+")
	return strings.ToUpper(reg.ReplaceAllString(s, "_"))
}

// normalizeID limpia y normaliza IDs de entidades para el grafo
func normalizeID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Reemplazar caracteres especiales y espacios con underscores
	reg, _ := regexp.Compile("[^a-z0-9_]+")
	s = reg.ReplaceAllString(s, "_")
	// Eliminar underscores duplicados y al inicio/final
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	s = strings.Trim(s, "_")
	return s
}

// extractKeywords genera una lista de palabras clave de un texto para búsqueda flexible
func extractKeywords(s string) []string {
	// Convertir a lowercase y extraer palabras alfanumericas
	s = strings.ToLower(strings.TrimSpace(s))
	reg, _ := regexp.Compile("[^a-z0-9]+")
	words := reg.Split(s, -1)

	// Filtrar palabras vacías y muy cortas (< 2 chars)
	var keywords []string
	seen := make(map[string]bool)
	for _, w := range words {
		if len(w) >= 2 && !seen[w] {
			keywords = append(keywords, w)
			seen[w] = true
		}
	}
	return keywords
}
