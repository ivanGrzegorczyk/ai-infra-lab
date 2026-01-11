package http

import (
	"context"
	"net/http"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
)

// Define una clave para el contexto para evitar colisiones
type contextKey string

const APIKeyConfigKey contextKey = "apiKeyConfig"

func AuthMiddleware(repo ports.KeyRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Extraer header
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				http.Error(w, "API Key requerida", http.StatusUnauthorized)
				return
			}

			// 2. Validar con el repositorio
			config, ok := repo.GetByKey(apiKey)
			if !ok {
				http.Error(w, "API Key inválida", http.StatusUnauthorized)
				return
			}

			// 3. Inyectar config en el contexto
			ctx := context.WithValue(r.Context(), APIKeyConfigKey, config)

			// 4. Continuar al siguiente handler con el nuevo contexto
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
