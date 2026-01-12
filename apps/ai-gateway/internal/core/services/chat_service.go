package services

import (
	"context"
	"fmt"
	"log"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
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

		// Definir el orden de proveedores basado en la preferencia
		var providers []ports.LLMProvider
		if req.PreferredProvider == "groq" {
			providers = []ports.LLMProvider{s.external, s.local}
		} else {
			providers = []ports.LLMProvider{s.local, s.external}
		}

		var lastErr error
		for i, provider := range providers {
			// Si es el segundo intento, enviar un evento SSE de "info"
			if i > 0 {
				resChan <- domain.ChatResponse{
					Content:  "⚠️ Cambiando de proveedor debido a un error...",
					Provider: "gateway-info",
				}
			}

			// Ejecutar el stream del proveedor
			providerResChan, providerErrChan := provider.GenerateStream(ctx, domain.ChatRequest{
				Messages:          req.Messages,
				PreferredProvider: provider.GetName(),
			})

			// Proxy del stream
			success := true
		streamLoop:
			for {
				select {
				case res, ok := <-providerResChan:
					if !ok {
						if success {
							return // Éxito completo
						}
						break streamLoop
					}
					resChan <- res
				case err := <-providerErrChan:
					if err != nil {
						lastErr = err
						success = false
						log.Printf("Error con proveedor %s: %v. Intentando fallback...", provider.GetName(), err)
					}
					break streamLoop
				case <-ctx.Done():
					return
				}

				if !success {
					break streamLoop
				}
			}

			if success {
				return
			}
		}

		errChan <- fmt.Errorf("todos los proveedores fallaron. Último error: %v", lastErr)
	}()

	return resChan, errChan
}

// proxyStream simplemente reenvía los datos de un canal a otro
func (s *chatService) proxyStream(outR chan domain.ChatResponse, outE chan error, inR <-chan domain.ChatResponse, inE <-chan error) {
	for {
		select {
		case r, ok := <-inR:
			if !ok {
				return
			}
			outR <- r
		case e := <-inE:
			if e != nil {
				outE <- e
			}
			return
		}
	}
}
