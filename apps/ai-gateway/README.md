# AI Gateway

[![Go Version](https://img.shields.io/badge/go-1.25+-00ADD8.svg)](https://golang.org)
[![Docker](https://img.shields.io/badge/docker-build-blue.svg)](Dockerfile)
[![Status](https://img.shields.io/badge/status-production--ready-green.svg)]()

El **AI Gateway** es el núcleo de la infraestructura de Inteligencia Artificial. Funciona como un orquestador inteligente de alto rendimiento diseñado para entornos Kubernetes, permitiendo el enrutamiento híbrido entre modelos locales (Ollama) y en la nube (Groq), con gestión de memoria persistente y protecciones de seguridad.

## 📋 Índice
1. [Características Principales](#-características-principales)
2. [Arquitectura y Flujo](#-arquitectura-y-flujo)
3. [Configuración](#-configuración)
4. [API Reference](#-api-reference)
5. [Gestión de Memoria y Contexto](#-gestión-de-memoria-y-contexto)
6. [Resiliencia y Safety Break](#-resiliencia-y-safety-break)
7. [Observabilidad](#-observabilidad)
8. [Despliegue y Desarrollo](#-despliegue-y-desarrollo)

---

## ⚡ Características Principales

* **Hybrid AI Routing:** Enrutamiento dinámico entre **Ollama** (privacidad/local) y **Groq** (velocidad/nube). Si Ollama no está disponible, el sistema puede hacer fallback transparente o notificar al usuario.
* **Session Memory (Redis):** Persistencia de conversaciones. El gateway "recuerda" el contexto de charlas pasadas mediante un `session_id` que vence tras 24 horas de inactividad.
* **Smart Summarization:** Gestión inteligente de la ventana de contexto. Si una conversación supera los **4096 tokens**, el sistema resume automáticamente los mensajes antiguos para mantener la coherencia sin romper el límite del modelo.
* **Safety Break:** Disyuntor de emergencia que corta la generación si un modelo entra en un bucle infinito (> 1500 tokens por respuesta).
* **Streaming Nativo:** Soporte total para **Server-Sent Events (SSE)**, entregando tokens en tiempo real con baja latencia.

---

## 🏗 Arquitectura y Flujo

El servicio sigue una **Arquitectura Hexagonal (Ports & Adapters)** para desacoplar la lógica de negocio de las implementaciones externas.

1.  **Entrada:** El request llega vía HTTP (`/v1/chat`). El `AuthMiddleware` valida la API Key contra el repositorio local.
2.  **Core:** El `ChatService` orquesta la lógica:
    * Recupera el historial de **Redis**.
    * Verifica el presupuesto de tokens (y resume si es necesario).
    * Selecciona el proveedor (Ollama/Groq).
3.  **Salida:** Los tokens se transmiten al cliente vía SSE mientras se capturan asíncronamente para actualizar el historial en Redis.

---

## 🛠 Configuración

El servicio se configura mediante variables de entorno y un archivo JSON para las credenciales de acceso.

### Variables de Entorno
| Variable | Descripción | Valor por Defecto |
| :--- | :--- | :--- |
| `PORT` | Puerto de escucha del servidor HTTP. | `8080` |
| `OLLAMA_URL` | URL del servicio Ollama (interno o externo). | `http://localhost:11434` |
| `GROQ_API_KEY` | API Key maestra para acceder a Groq Cloud. | `""` |
| `REDIS_ADDR` | Dirección del servidor Redis para sesiones. | `redis-service.ai-lab:6379` |

### Gestión de API Keys (`configs/keys.json`)
El acceso al Gateway se controla mediante un archivo JSON que define las keys válidas y sus permisos.

```json
{
  "keys": [
    {
      "key": "sk-...",
      "name": "admin-user",
      "allowed_providers": ["ollama", "groq"]
    },
    {
      "key": "sk-...",
      "name": "guest-user",
      "allowed_providers": ["ollama"]
    }
  ]
}
```

---

## 🔌 API Reference

### Chat Completions
Endpoint principal para interactuar con los modelos. La respuesta es un stream de eventos (SSE).

`POST /v1/chat`

#### Headers
* `Content-Type`: `application/json`
* `X-API-Key`: `<tu-api-key>`

#### Body Parameters
| Parámetro | Tipo | Requerido | Descripción |
| :--- | :--- | :--- | :--- |
| `session_id` | `string` | No (Recomendado) | UUID para mantener memoria. Si se omite, la charla es "stateless". |
| `preferred_provider` | `string` | Sí | `groq` u `ollama`. |
| `messages` | `array` | Sí | Lista de objetos `{role: "user", content: "..."}`. |

#### Ejemplos de Uso (CURL)

**1. Chat Básico (Stateless)**
```bash
curl -N -X POST http://localhost:8080/v1/chat \
  -H "Content-Type: application/json" \
  -H "X-API-Key: {api_key}" \
  -d '{
    "preferred_provider": "groq",
    "messages": [
      { "role": "user", "content": "Explica la teoría de cuerdas en una frase." }
    ]
  }'
```

**2. Chat con Memoria (Session ID)**
```bash
# Mensaje 1: Me presento
curl -N -X POST http://localhost:8080/v1/chat \
  -H "Content-Type: application/json" \
  -H "X-API-Key: {api_key}" \
  -d '{
    "session_id": "session-dev-001",
    "preferred_provider": "ollama",
    "messages": [
      { "role": "user", "content": "Hola, soy Iván." }
    ]
  }'

# Mensaje 2: Verifico memoria
curl -N -X POST http://localhost:8080/v1/chat \
  -H "Content-Type: application/json" \
  -H "X-API-Key: {api_key}" \
  -d '{
    "session_id": "session-dev-001",
    "preferred_provider": "ollama",
    "messages": [
      { "role": "user", "content": "¿Cómo me llamo?" }
    ]
  }'
```

---

## 🧠 Gestión de Memoria y Contexto

El Gateway transforma modelos *stateless* (sin memoria) en agentes con memoria persistente.

1.  **Redis Store:** Cada interacción se guarda en Redis con un TTL (Time-To-Live) de 24 horas.
2.  **Límite de Contexto:** El sistema maneja una ventana de **4096 tokens**.
3.  **Algoritmo de Resumen (Summarization):**
    * Antes de cada request, se calculan los tokens acumulados usando `tiktoken`.
    * Si `TotalTokens > (4096 - 1000)`, se dispara el proceso de resumen.
    * Los mensajes antiguos se envían a un modelo rápido (Groq) para generar una síntesis comprimida.
    * El historial se reemplaza por: `[System: Resumen previo] + [Último mensaje usuario]`.

---

## 🛡 Resiliencia y Safety Break

Para proteger la infraestructura de errores de los modelos (alucinaciones o bucles infinitos), el Gateway implementa un **Safety Break**.

* **Límite de Generación:** Máximo **2000 tokens** por respuesta individual.
* **Comportamiento:** Si un modelo excede este límite en una sola respuesta, el Gateway corta el stream automáticamente y envía una señal de `[DONE]`. Esto previene el consumo excesivo de recursos y bloqueos de red.

---

## 📊 Observabilidad

El servicio expone métricas en formato Prometheus en `/metrics`.

| Métrica | Tipo | Descripción |
| :--- | :--- | :--- |
| `ai_gateway_tokens_total` | Counter | Total de tokens generados (labels: `user`, `provider`). |
| `ai_gateway_ttft_seconds` | Histogram | Tiempo hasta el primer token (latencia percibida). |
| `ai_gateway_http_requests_total` | Counter | Total de requests HTTP (labels: `status`, `user`, `provider`). |
| `ai_gateway_groq_remaining_tokens` | Gauge | Cuota de tokens restante en la API de Groq. |
| `ai_gateway_tokens_per_request` | Histogram | Distribución del tamaño de las respuestas. |

---

## 🚀 Despliegue y Desarrollo

### Ejecución Local (Docker)
```bash
# 1. Levantar Redis
docker run -d -p 6379:6379 --name redis redis:alpine

# 2. Configurar entorno
export REDIS_ADDR="localhost:6379"
export GROQ_API_KEY="gsk_..."

# 3. Ejecutar Gateway
go run cmd/gateway/main.go
```

### Construcción de Imagen
```bash
docker build -t ivangrz/ai-gateway:v1.0.0 .
```
