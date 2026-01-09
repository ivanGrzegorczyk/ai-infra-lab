package services

import (
	"context"

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

func (s *chatService) ExecuteChat(ctx context.Context, req domain.ChatRequest) (<-chan domain.ChatResponse, <-chan error) {
	// 1. Intentamos con el proveedor local (Ollama)
	resChan, errChan := s.local.GenerateStream(ctx, req)

	// Creamos canales de salida que nosotros controlamos
	outRes := make(chan domain.ChatResponse)
	outErr := make(chan error, 1)

	go func() {
		defer close(outRes)
		defer close(outErr)

		// Escuchamos el primer evento del proveedor local
		select {
		case err := <-errChan:
			if err != nil {
				// Si Ollama falla de entrada (saturación/down), disparamos Groq
				fallbackRes, fallbackErr := s.external.GenerateStream(ctx, req)
				s.proxyStream(outRes, outErr, fallbackRes, fallbackErr)
				return
			}
		case res, ok := <-resChan:
			if !ok {
				return
			}
			// Si el primer token llega bien, seguimos con Ollama
			outRes <- res
			s.proxyStream(outRes, outErr, resChan, errChan)
		case <-ctx.Done():
			return
		}
	}()

	return outRes, outErr
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
