package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
)

// Define una clave para el contexto para evitar colisiones
type contextKey string

const APIKeyConfigKey contextKey = "apiKeyConfig"

func AuthMiddleware(repo ports.KeyRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 1. Intentar extraer del header propietario "X-API-Key"
			apiKey := r.Header.Get("X-API-Key")

			// 2. Si no está, intentar extraer del estándar "Authorization: Bearer <token>"
			if apiKey == "" {
				authHeader := r.Header.Get("Authorization")
				if strings.HasPrefix(authHeader, "Bearer ") {
					apiKey = strings.TrimPrefix(authHeader, "Bearer ")
				}
			}

			// 3. Si sigue vacío, error
			if apiKey == "" {
				http.Error(w, "API Key requerida (Use header 'X-API-Key' o 'Authorization: Bearer')", http.StatusUnauthorized)
				return
			}

			// 4. Validar con el repositorio
			config, ok := repo.GetByKey(apiKey)
			if !ok {
				http.Error(w, "API Key inválida", http.StatusUnauthorized)
				return
			}

			// 5. Inyectar config en el contexto
			ctx := context.WithValue(r.Context(), APIKeyConfigKey, config)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
