package services

import (
	"context"
	"fmt"
	"log"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/observability"
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
}

func NewChatService(local, external ports.LLMProvider) ports.ChatService {
	return &chatService{
		local:    local,
		external: external,
	}
}

func (s *chatService) ExecuteChat(ctx context.Context, req domain.ChatRequest, keyConfig domain.APIKeyConfig) (<-chan domain.ChatResponse, <-chan error) {
	resChan := make(chan domain.ChatResponse)
	errChan := make(chan error, 1)

	go func() {
		defer close(resChan)
		defer close(errChan)

		isAllowed := s.createPermissionChecker(keyConfig.AllowedProviders)

		// Validar si el proveedor solicitado está permitido
		if req.PreferredProvider != "" && !isAllowed(req.PreferredProvider) {
			observability.ForbiddenProviderAttempts.WithLabelValues(
				keyConfig.Name,
				req.PreferredProvider,
			).Inc()

			resChan <- domain.ChatResponse{
				Content:  fmt.Sprintf("ℹ️ Tu API Key no tiene acceso a '%s'...", req.PreferredProvider),
				Provider: ProviderInfoName,
			}
		}

		providers := s.selectProviders(req.PreferredProvider, keyConfig)

		if len(providers) == 0 {
			errChan <- fmt.Errorf(MsgNoPermissions)
			return
		}

		if err := s.executeWithFallback(ctx, req, providers, resChan); err != nil {
			errChan <- err
		}
	}()

	return resChan, errChan
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

	for {
		select {
		case res, ok := <-providerResChan:
			if !ok {
				return true, nil // Stream completado exitosamente
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
