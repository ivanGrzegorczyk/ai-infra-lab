package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
)

// Constantes para nombres de proveedores y mensajes
const (
	ProviderGroq   = "groq"
	ProviderOllama = "ollama"

	ProviderInfoName = "gateway-info"

	MsgNoPermissions      = "tu API Key no tiene permisos para ningún proveedor de IA"
	MsgSwitchingProvider  = "⚠️ Cambiando de proveedor debido a un error..."
	MsgAllProvidersFailed = "todos los proveedores fallaron. Último error: %v"
	MsgProviderError      = "Error con proveedor %s: %v. Intentando fallback..."
)

type chatService struct {
	local    ports.LLMProvider // Ollama
	external ports.LLMProvider // Groq
	sessions ports.SessionRepository
}

func NewChatService(local, external ports.LLMProvider, sessions ports.SessionRepository) ports.ChatService {
	return &chatService{
		local:    local,
		external: external,
		sessions: sessions,
	}
}

func (s *chatService) ExecuteChat(ctx context.Context, req domain.ChatRequest, keyConfig domain.APIKeyConfig, resChan chan domain.ChatResponse) error {
	var fullHistory []domain.ChatMessage

	// 1. Recuperar historial si hay session_id
	if req.SessionID != "" {
		history, err := s.sessions.GetHistory(ctx, req.SessionID)
		if err == nil && len(history) > 0 {
			fullHistory = history
		}
	}

	// 2. Unir el historial con los nuevos mensajes del usuario
	fullHistory = append(fullHistory, req.Messages...)

	// Preparamos la request "aumentada" para los proveedores
	providerReq := req
	providerReq.Messages = fullHistory

	providers := s.getOrderedProviders(req.PreferredProvider, keyConfig)
	if len(providers) == 0 {
		return fmt.Errorf(MsgNoPermissions)
	}

	var lastErr error
	for i, provider := range providers {
		if i > 0 {
			s.sendProviderSwitchNotification(resChan)
		}

		// 3. Captura del stream para Redis
		// Usamos un canal intermedio para "espiar" lo que dice la IA
		// y poder guardarlo en el historial al terminar.
		proxyChan := make(chan domain.ChatResponse)
		var assistantResponseAccumulator string

		// Goroutine para pasar del proxy al canal real y acumular el texto
		go func() {
			for res := range proxyChan {
				if res.Provider != ProviderInfoName {
					assistantResponseAccumulator += res.Content
				}
				resChan <- res
			}
		}()

		success, err := s.streamFromProvider(ctx, providerReq, provider, proxyChan)
		close(proxyChan)

		if success {
			// 4. Guardar la conversación completa en Redis
			if req.SessionID != "" {
				fullHistory = append(fullHistory, domain.ChatMessage{
					Role:    "assistant",
					Content: assistantResponseAccumulator,
				})
				_ = s.sessions.SaveHistory(ctx, req.SessionID, fullHistory)
			}
			return nil
		}

		lastErr = err
		log.Printf(MsgProviderError, provider.GetName(), err)
	}

	return fmt.Errorf(MsgAllProvidersFailed, lastErr)
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

// executeWithFallback intenta los proveedores en orden hasta que uno tenga éxito
func (s *chatService) executeWithFallback(ctx context.Context, req domain.ChatRequest, providers []ports.LLMProvider, resChan chan domain.ChatResponse) error {
	var lastErr error

	for i, provider := range providers {
		if i > 0 {
			s.sendProviderSwitchNotification(resChan)
		}

		success, err := s.streamFromProvider(ctx, req, provider, resChan)
		if success {
			return nil
		}

		lastErr = err
		log.Printf(MsgProviderError, provider.GetName(), err)
	}

	return fmt.Errorf(MsgAllProvidersFailed, lastErr)
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

	for {
		select {
		case res, ok := <-providerResChan:
			if !ok {
				return true, nil // Stream completado exitosamente
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
