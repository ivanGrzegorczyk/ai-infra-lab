package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Contador de Tokens (Segmentado por modelo y proveedor)
	TokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ai_gateway_tokens_total",
		Help: "Total de tokens generados",
	}, []string{"model", "provider"})

	// Histograma de TTFT (Time To First Token)
	TimeToFirstToken = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ai_gateway_ttft_seconds",
		Help:    "Tiempo hasta el primer token",
		Buckets: prometheus.DefBuckets,
	}, []string{"model", "provider"})

	// Contador de Requests
	HttpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ai_gateway_http_requests_total",
		Help: "Total de peticiones HTTP",
	}, []string{"status", "model", "provider"})
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
)
