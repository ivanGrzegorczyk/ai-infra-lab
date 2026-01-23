package services

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/observability"
	"github.com/pkoukk/tiktoken-go"
)

const (
	ProviderGroq     = "groq"
	ProviderOllama   = "ollama"
	ProviderInfoName = "gateway-info"

	MsgNoPermissions      = "tu API Key no tiene permisos para ningun proveedor de IA"
	MsgSwitchingProvider  = "Cambiando de proveedor debido a un error..."
	MsgAllProvidersFailed = "todos los proveedores fallaron. Ultimo error: %v"
	MsgProviderError      = "Error con proveedor %s: %v. Intentando fallback..."
)

type chatService struct {
	local       ports.LLMProvider
	external    ports.LLMProvider
	sessions    ports.SessionRepository
	embedder    ports.EmbeddingGenerator
	vectorStore ports.VectorStore
	graphRepo   ports.GraphRepository
}

func NewChatService(local, external ports.LLMProvider, sessions ports.SessionRepository, embedder ports.EmbeddingGenerator, store ports.VectorStore, graphRepo ports.GraphRepository) ports.ChatService {
	return &chatService{
		local:       local,
		external:    external,
		sessions:    sessions,
		embedder:    embedder,
		vectorStore: store,
		graphRepo:   graphRepo,
	}
}

func (s *chatService) ExecuteChat(ctx context.Context, req domain.ChatRequest, keyConfig domain.APIKeyConfig, resChan chan domain.ChatResponse) error {
	var fullHistory []domain.ChatMessage

	// 1. Recuperar historial
	if req.SessionID != "" {
		history, err := s.sessions.GetHistory(ctx, req.SessionID)
		if err == nil && len(history) > 0 {
			fullHistory = history
		}
	}

	// 2. Lógica RAG (Retrieval)
	lastUserMsg := ""
	if len(req.Messages) > 0 {
		lastUserMsg = req.Messages[len(req.Messages)-1].Content
	}

	// 2a. VECTOR RAG (Qdrant)
	vectorContext := ""
	if lastUserMsg != "" {
		log.Printf("RAG Vector: Busca contexto para query: '%s'", lastUserMsg)

		docs, err := s.retrieveContext(ctx, lastUserMsg)
		if err != nil {
			log.Printf("RAG Vector Error: %v", err)
		} else if len(docs) > 0 {
			log.Printf("RAG Vector: Se encontraron %d documentos relevantes.", len(docs))
			vectorContext = s.formatContext(docs)
		} else {
			log.Printf("RAG Vector: No se encontro contexto suficiente.")
		}
	}

	// 2b. GRAPH RAG (Neo4j)
	graphContext := ""
	if lastUserMsg != "" && s.graphRepo != nil {
		graphContext = s.retrieveGraphContext(ctx, lastUserMsg)
	}

	// Combinar contextos
	contextBlock := ""
	if vectorContext != "" || graphContext != "" {
		var sb strings.Builder
		if vectorContext != "" {
			sb.WriteString(vectorContext)
		}
		if graphContext != "" {
			sb.WriteString("\n")
			sb.WriteString(graphContext)
		}
		contextBlock = sb.String()
	}

	// 3. Preparar mensajes para inferencia
	var messagesForInference []domain.ChatMessage

	if contextBlock != "" {
		// Inyectamos el contexto como un System Prompt temporal
		systemMsg := domain.ChatMessage{
			Role: "system",
			Content: fmt.Sprintf(`Eres un asistente experto. Utiliza el siguiente contexto recuperado para responder la pregunta del usuario.
Si la respuesta no se encuentra en el contexto, di "No tengo información sobre eso en mis documentos".

--- CONTEXTO ---
%s
----------------
`, contextBlock),
		}
		messagesForInference = append(messagesForInference, systemMsg)
	}

	messagesForInference = append(messagesForInference, fullHistory...)
	messagesForInference = append(messagesForInference, req.Messages...)

	// Validación básica de tokens
	if s.countTokens(messagesForInference) > (4096 - 1000) {
		if len(fullHistory) > 2 {
			log.Println("Contexto muy largo, recortando historial antiguo...")
			messagesForInference = messagesForInference[len(messagesForInference)/2:]
		}
	}

	providerReq := req
	providerReq.Messages = messagesForInference

	providers := s.selectProviders(req.PreferredProvider, keyConfig)
	if len(providers) == 0 {
		return fmt.Errorf(MsgNoPermissions)
	}

	var lastErr error
	for i, provider := range providers {
		if i > 0 {
			s.sendProviderSwitchNotification(resChan)
		}

		proxyChan := make(chan domain.ChatResponse)
		var assistantResponseAccumulator string
		var wg sync.WaitGroup
		wg.Add(1)

		go func() {
			defer wg.Done()
			for res := range proxyChan {
				if res.Provider != ProviderInfoName {
					assistantResponseAccumulator += res.Content
				}
				resChan <- res
			}
		}()

		success, err := s.streamFromProvider(ctx, providerReq, provider, proxyChan)
		close(proxyChan)
		wg.Wait()

		if success {
			// Guardar en Redis solo la interacción limpia (sin el bloque de contexto gigante)
			if req.SessionID != "" {
				userMsg := req.Messages[len(req.Messages)-1]
				newExchange := []domain.ChatMessage{
					userMsg,
					{Role: "assistant", Content: assistantResponseAccumulator},
				}
				fullHistory = append(fullHistory, newExchange...)
				_ = s.sessions.SaveHistory(ctx, req.SessionID, fullHistory)
			}
			return nil
		}

		lastErr = err
		log.Printf(MsgProviderError, provider.GetName(), err)
	}

	return fmt.Errorf(MsgAllProvidersFailed, lastErr)
}

func (s *chatService) retrieveContext(ctx context.Context, query string) ([]domain.VectorDocument, error) {
	start := time.Now()

	vector, err := s.embedder.GenerateEmbedding(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("error generando embedding: %w", err)
	}

	// Busca Top 3 en la colección knowledge_base
	results, err := s.vectorStore.Search(ctx, "knowledge_base", vector, 3)
	if err != nil {
		return nil, fmt.Errorf("error buscando en vector store: %w", err)
	}

	// Registrar métrica de duración
	observability.RAGVectorSearchDuration.Observe(time.Since(start).Seconds())

	var relevantDocs []domain.VectorDocument
	for _, res := range results {
		log.Printf("   -> RAG Match: Score %.4f", res.Score)
		if res.Score > 0.25 {
			relevantDocs = append(relevantDocs, res.Document)
		}
	}

	// Registrar métrica de documentos encontrados
	observability.RAGDocumentsFound.Observe(float64(len(relevantDocs)))

	return relevantDocs, nil
}

func (s *chatService) formatContext(docs []domain.VectorDocument) string {
	var sb strings.Builder
	for _, doc := range docs {
		source := "doc"
		if val, ok := doc.Metadata["filename"]; ok {
			source = fmt.Sprintf("%v", val)
		}
		sb.WriteString(fmt.Sprintf("[Fuente: %s]: %s\n\n", source, doc.Content))
	}
	return sb.String()
}

// retrieveGraphContext busca relaciones en Neo4j basándose en entidades mencionadas en el prompt
func (s *chatService) retrieveGraphContext(ctx context.Context, query string) string {
	start := time.Now()

	// Tokenizar el query para buscar por palabras individuales
	queryWords := tokenizeQuery(query)

	// Estrategia mejorada:
	// 1. Buscar por ID normalizado en el prompt
	// 2. Buscar por nombre original
	// 3. Buscar por keywords (cada palabra del query vs palabras del nodo)
	cypherQuery := `
		MATCH (n:Entity)
		WHERE toLower($prompt) CONTAINS n.id
		   OR toLower($prompt) CONTAINS toLower(n.name)
		   OR ANY(w IN $words WHERE n.id CONTAINS w)
		   OR ANY(w IN $words WHERE ANY(kw IN coalesce(n.keywords, []) WHERE kw CONTAINS w OR w CONTAINS kw))
		OPTIONAL MATCH (n)-[r]->(m:Entity)
		OPTIONAL MATCH (p:Entity)-[r2]->(n)
		WITH n, collect(DISTINCT {source: n.name, rel: type(r), target: m.name}) AS outRels,
		     collect(DISTINCT {source: p.name, rel: type(r2), target: n.name}) AS inRels
		RETURN n.name AS entity, n.id AS entityId, outRels, inRels
		LIMIT 5
	`
	params := map[string]interface{}{
		"prompt": strings.ToLower(query),
		"words":  queryWords,
	}

	results, err := s.graphRepo.ExecuteQuery(ctx, cypherQuery, params)

	// Registrar métrica de duración
	observability.RAGGraphSearchDuration.Observe(time.Since(start).Seconds())

	if err != nil {
		log.Printf("RAG Graph Error: %v", err)
		observability.RAGRelationsFound.Observe(0)
		return ""
	}

	if len(results) == 0 {
		log.Printf("RAG Graph: No se encontraron entidades para query: '%s' (words: %v)", query, queryWords)
		observability.RAGRelationsFound.Observe(0)
		return ""
	}

	log.Printf("RAG Graph: Query tokenizado en palabras: %v", queryWords)

	var sb strings.Builder
	sb.WriteString("RELACIONES DEL GRAFO DE CONOCIMIENTO:\n")
	relCount := 0

	for _, row := range results {
		entityName, _ := row["entity"].(string)

		// Procesar relaciones salientes
		if outRels, ok := row["outRels"].([]interface{}); ok {
			for _, rel := range outRels {
				if relMap, ok := rel.(map[string]interface{}); ok {
					target, _ := relMap["target"].(string)
					relType, _ := relMap["rel"].(string)
					if target != "" && relType != "" {
						sb.WriteString(fmt.Sprintf("- %s -[%s]-> %s\n", entityName, relType, target))
						relCount++
					}
				}
			}
		}

		// Procesar relaciones entrantes
		if inRels, ok := row["inRels"].([]interface{}); ok {
			for _, rel := range inRels {
				if relMap, ok := rel.(map[string]interface{}); ok {
					source, _ := relMap["source"].(string)
					relType, _ := relMap["rel"].(string)
					if source != "" && relType != "" {
						sb.WriteString(fmt.Sprintf("- %s -[%s]-> %s\n", source, relType, entityName))
						relCount++
					}
				}
			}
		}
	}

	if relCount == 0 {
		log.Printf("RAG Graph: Entidades encontradas pero sin relaciones.")
		observability.RAGRelationsFound.Observe(0)
		return ""
	}

	// Registrar métrica de relaciones encontradas
	observability.RAGRelationsFound.Observe(float64(relCount))

	graphContext := sb.String()
	log.Printf("RAG Graph: Se encontraron %d relaciones para %d entidades.", relCount, len(results))
	log.Printf("RAG Graph Context:\n%s", graphContext)
	return graphContext
}

// tokenizeQuery extrae palabras significativas del query para búsqueda flexible
func tokenizeQuery(query string) []string {
	// Palabras de parada en español e inglés
	stopWords := map[string]bool{
		"el": true, "la": true, "los": true, "las": true, "un": true, "una": true,
		"de": true, "del": true, "en": true, "con": true, "para": true, "por": true,
		"que": true, "qué": true, "como": true, "cómo": true, "es": true, "son": true,
		"se": true, "y": true, "o": true, "a": true, "al": true, "lo": true,
		"the": true, "is": true, "are": true, "and": true, "or": true, "for": true,
		"to": true, "in": true, "on": true, "of": true, "with": true, "how": true,
		"what": true, "which": true, "this": true, "that": true, "it": true,
		"usa": true, "usar": true, "hace": true, "tiene": true, "hay": true,
	}

	query = strings.ToLower(query)
	// Extraer solo palabras alfanumericas
	reg, _ := regexp.Compile("[^a-záéíóúñ0-9]+")
	words := reg.Split(query, -1)

	var result []string
	seen := make(map[string]bool)
	for _, w := range words {
		// Filtrar palabras muy cortas, stopwords y duplicados
		if len(w) >= 3 && !stopWords[w] && !seen[w] {
			result = append(result, w)
			seen[w] = true
		}
	}
	return result
}

// selectProviders devuelve la lista de proveedores en orden de prioridad según preferencias y permisos
func (s *chatService) selectProviders(preferredProvider string, keyConfig domain.APIKeyConfig) []ports.LLMProvider {
	isAllowed := s.createPermissionChecker(keyConfig.AllowedProviders)

	var providers []ports.LLMProvider

	if preferredProvider == ProviderGroq {
		providers = s.addProviderIfAllowed(providers, s.external, ProviderGroq, isAllowed)
		providers = s.addProviderIfAllowed(providers, s.local, ProviderOllama, isAllowed)
	} else {
		providers = s.addProviderIfAllowed(providers, s.local, ProviderOllama, isAllowed)
		providers = s.addProviderIfAllowed(providers, s.external, ProviderGroq, isAllowed)
	}

	return providers
}

// createPermissionChecker devuelve una función que verifica si un proveedor está permitido
func (s *chatService) createPermissionChecker(allowedProviders []string) func(string) bool {
	return func(provider string) bool {
		for _, allowed := range allowedProviders {
			if allowed == provider {
				return true
			}
		}
		return false
	}
}

// addProviderIfAllowed agrega un proveedor a la lista si está permitido
func (s *chatService) addProviderIfAllowed(providers []ports.LLMProvider, provider ports.LLMProvider, providerName string, isAllowed func(string) bool) []ports.LLMProvider {
	if isAllowed(providerName) {
		return append(providers, provider)
	}
	return providers
}

// sendProviderSwitchNotification envía un mensaje de notificación sobre el cambio de proveedor
func (s *chatService) sendProviderSwitchNotification(resChan chan domain.ChatResponse) {
	resChan <- domain.ChatResponse{
		Content:  MsgSwitchingProvider,
		Provider: ProviderInfoName,
	}
}

// streamFromProvider transmite respuestas desde un único proveedor
func (s *chatService) streamFromProvider(ctx context.Context, req domain.ChatRequest, provider ports.LLMProvider, resChan chan domain.ChatResponse) (bool, error) {
	providerResChan, providerErrChan := provider.GenerateStream(ctx, domain.ChatRequest{
		Messages:          req.Messages,
		PreferredProvider: provider.GetName(),
	})

	const msPerChar = 20 * time.Millisecond
	const maxTokensPerTurn = 1500
	tokensInThisTurn := 0

	for {
		select {
		case res, ok := <-providerResChan:
			if !ok {
				return true, nil // Stream completado exitosamente
			}

			// --- SAFETY BREAK (evita hallucination loops) ---
			tokensInThisTurn++
			if tokensInThisTurn > maxTokensPerTurn {
				log.Printf("SAFETY BREAK: Proveedor %s excedió el límite de seguridad.", provider.GetName())
				return true, nil // Corta el stream pero lo da como "exitoso" para no disparar fallbacks innecesarios
			}

			// Si el proveedor es Groq, aplica el delay por caracter para suavizar la lectura
			if res.Provider == "groq" {
				charCount := len(res.Content)
				if charCount > 0 {
					time.Sleep(time.Duration(charCount) * msPerChar)
				}
			}

			resChan <- res

		case err := <-providerErrChan:
			if err != nil {
				return false, err
			}
			return true, nil

		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

// countTokens estima el total de tokens en una lista de mensajes
func (s *chatService) countTokens(messages []domain.ChatMessage) int {
	tkm, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return len(strings.Join(func() []string {
			var s []string
			for _, m := range messages {
				s = append(s, m.Content)
			}
			return s
		}(), " ")) / 4 // Fallback burdo: 4 caracteres aprox 1 token
	}

	count := 0
	for _, msg := range messages {
		count += len(tkm.Encode(msg.Content, nil, nil))
		count += 4 // Overhead por mensaje
	}
	return count
}

// summarizeHistory toma los mensajes viejos y genera un resumen compacto
func (s *chatService) summarizeHistory(ctx context.Context, history []domain.ChatMessage) (domain.ChatMessage, error) {
	log.Println("Contexto excedido. Generando resumen intermedio...")

	promptResumen := "Resume la siguiente conversación de forma muy breve y técnica, manteniendo los datos clave: \n"
	for _, msg := range history {
		promptResumen += fmt.Sprintf("%s: %s\n", msg.Role, msg.Content)
	}

	// Usa un canal temporal para el resumen
	tempChan := make(chan domain.ChatResponse)
	summaryResult := ""

	go func() {
		for res := range tempChan {
			summaryResult += res.Content
		}
	}()

	// Uso groq el más rápido
	reqResumen := domain.ChatRequest{
		Messages: []domain.ChatMessage{{Role: "user", Content: promptResumen}},
	}

	// Llama directamente al provider externo (Groq)
	_, err := s.streamFromProvider(ctx, reqResumen, s.external, tempChan)
	close(tempChan)

	if err != nil {
		return domain.ChatMessage{}, err
	}

	return domain.ChatMessage{
		Role:    "system",
		Content: "Resumen de la charla anterior: " + summaryResult,
	}, nil
}
