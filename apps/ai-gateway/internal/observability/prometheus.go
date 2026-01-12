package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Contador de Tokens (Segmentado por modelo y proveedor)
	TokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ai_gateway_tokens_total",
		Help: "Total de tokens generados por usuario y proveedor",
	}, []string{"user", "provider"})

	// Histograma de TTFT (Time To First Token)
	TimeToFirstToken = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ai_gateway_ttft_seconds",
		Help:    "Tiempo hasta el primer token (TTFT) por usuario y proveedor",
		Buckets: []float64{0.1, 0.5, 1, 2, 5},
	}, []string{"user", "provider"})

	// Contador de Requests
	HttpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ai_gateway_http_requests_total",
		Help: "Total de peticiones HTTP segmentadas por status, usuario y proveedor",
	}, []string{"status", "user", "provider"})
	// Gauge para tokens restantes de Groq
	GroqRemainingTokens = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ai_gateway_groq_remaining_tokens",
		Help: "Tokens restantes en la ventana actual de Groq",
	})
	// Gauge para requests restantes de Groq
	GroqRemainingRequests = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ai_gateway_groq_remaining_requests",
		Help: "Requests restantes en el día para Groq",
	})
	// Histograma de Tokens por Request
	TokensPerRequest = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ai_gateway_tokens_per_request",
		Help:    "Distribución de tokens generados por cada petición",
		Buckets: []float64{10, 50, 100, 250, 500, 1000},
	}, []string{"user", "provider"})
	ForbiddenProviderAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ai_gateway_forbidden_provider_attempts_total",
		Help: "Intentos de acceso a proveedores no autorizados por la API Key",
	}, []string{"user", "requested_provider"})
)
