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
5. [RAG: Ingesta y Búsqueda Semántica](#-rag-ingesta-y-búsqueda-semántica)
6. [GraphRAG: Knowledge Graph](#-graphrag-knowledge-graph)
7. [Gestión de Memoria y Contexto](#-gestión-de-memoria-y-contexto)
8. [Resiliencia y Safety Break](#-resiliencia-y-safety-break)
9. [Observabilidad](#-observabilidad)
10. [Despliegue y Desarrollo](#-despliegue-y-desarrollo)

---

## ⚡ Características Principales

* **Hybrid AI Routing:** Enrutamiento dinámico entre **Ollama** (privacidad/local) y **Groq** (velocidad/nube). Si Ollama no está disponible, el sistema puede hacer fallback transparente o notificar al usuario.
* **Session Memory (Redis):** Persistencia de conversaciones. El gateway "recuerda" el contexto de charlas pasadas mediante un `session_id` que vence tras 24 horas de inactividad.
* **RAG (Retrieval-Augmented Generation):** Sistema completo de ingesta de documentos, vectorización automática con embeddings (nomic-embed-text) y búsqueda semántica en Qdrant para enriquecer las respuestas con conocimiento externo.
* **GraphRAG (Knowledge Graph):** Extracción automática de entidades y relaciones desde documentos usando LLM. Almacenamiento en Neo4j con búsqueda flexible por keywords tokenizados. Combina Vector RAG + Graph RAG para respuestas más precisas.
* **Smart Summarization:** Gestión inteligente de la ventana de contexto. Si una conversación supera los **4096 tokens**, el sistema resume automáticamente los mensajes antiguos para mantener la coherencia sin romper el límite del modelo.
* **Safety Break:** Disyuntor de emergencia que corta la generación si un modelo entra en un bucle infinito (> 1500 tokens por respuesta).
* **Streaming Nativo:** Soporte total para **Server-Sent Events (SSE)**, entregando tokens en tiempo real con baja latencia.

---

## 🏗 Arquitectura y Flujo

El servicio sigue una **Arquitectura Hexagonal (Ports & Adapters)** para desacoplar la lógica de negocio de las implementaciones externas.

1.  **Entrada:** El request llega vía HTTP (`/v1/chat`). El `AuthMiddleware` valida la API Key contra el repositorio local.
2.  **Core:** El `ChatService` orquesta la lógica:
    * Recupera el historial de **Redis**.
    * Ejecuta **Hybrid RAG**: búsqueda vectorial en Qdrant + búsqueda de relaciones en Neo4j.
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
| `QDRANT_ADDR` | Dirección del servidor Qdrant para RAG. | `qdrant-service.ai-lab:6334` |
| `NEO4J_URI` | URI de conexión a Neo4j para GraphRAG. | `bolt://neo4j-service:7687` |
| `NEO4J_AUTH` | Credenciales de Neo4j (formato `user/password`). | `neo4j/password` |

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

## 🔍 RAG: Ingesta y Búsqueda Semántica

El Gateway implementa un sistema completo de **Retrieval-Augmented Generation (RAG)** que permite alimentar a los modelos con conocimiento externo almacenado en documentos.

### Flujo de Ingesta de Documentos

1. **Recepción:** El endpoint `/v1/ingest` recibe texto plano (`.txt`, `.md`, `.json`).
2. **Validación:** Se verifica que el contenido sea texto UTF-8 válido (rechaza binarios).
3. **Job Asíncrono:** Se crea un job con estado `pending` y se guarda en Redis.
4. **Chunking:** El texto se divide en fragmentos de ~500 caracteres con overlap de 50.
5. **Vectorización:** Cada chunk se convierte en un embedding usando `nomic-embed-text` (768 dimensiones) vía Ollama.
6. **Almacenamiento:** Los vectores se guardan en Qdrant con metadata (filename, chunk_index, job_id).
7. **Finalización:** El job se marca como `completed` o `failed` según el resultado.

### Flujo de Búsqueda (Durante el Chat) - Hybrid RAG

El sistema combina **Vector RAG + Graph RAG** para obtener el mejor contexto posible:

1. **Vector RAG (Qdrant):**
   - Se genera el embedding de la pregunta del usuario.
   - Se buscan los Top 3 documentos más similares (score > 0.25).
   - Los fragmentos relevantes se añaden como contexto.

2. **Graph RAG (Neo4j):**
   - Se tokeniza la pregunta eliminando stopwords.
   - Se buscan entidades que coincidan con las palabras clave.
   - Se expanden las relaciones directas de cada entidad encontrada.
   - Las relaciones se añaden como contexto estructurado.

3. **Generación Enriquecida:** El modelo responde con:
   - Conocimiento de documentos (Vector RAG)
   - Relaciones explícitas entre conceptos (Graph RAG)
   - Su entrenamiento base

### Endpoints de RAG

#### Ingestar Documento
`POST /v1/ingest`

**Headers:**
* `Content-Type`: `application/json`
* `X-API-Key`: `<tu-api-key>`

**Body:**
```json
{
  "content": "Texto completo del documento...",
  "metadata": {
    "filename": "manual.txt",
    "author": "Ivan"
  }
}
```

**Respuesta (202 Accepted):**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending",
  "created_at": "2026-01-18T10:30:00Z",
  "metadata": { "filename": "manual.txt" }
}
```

#### Consultar Estado del Job
`GET /v1/ingest/status/{job_id}`

**Headers:**
* `X-API-Key`: `<tu-api-key>`

**Respuesta:**
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "completed",
  "message": "Procesados 25 chunks exitosamente",
  "created_at": "2026-01-18T10:30:00Z"
}
```

**Estados posibles:** `pending`, `processing`, `completed`, `failed`

### Ejemplo Completo de Uso

```bash
# 1. Ingestar un documento
curl -X POST http://localhost:8080/v1/ingest \
  -H "Content-Type: application/json" \
  -H "X-API-Key: sk-..." \
  -d '{
    "content": "Kubernetes es un sistema de orquestación de contenedores...",
    "metadata": { "filename": "k8s-basics.txt" }
  }'

# Respuesta: {"id": "abc-123", "status": "pending", ...}

# 2. Consultar estado (repetir hasta ver "completed")
curl http://localhost:8080/v1/ingest/status/abc-123 \
  -H "X-API-Key: sk-..."

# 3. Hacer preguntas relacionadas al documento
curl -N -X POST http://localhost:8080/v1/chat \
  -H "Content-Type: application/json" \
  -H "X-API-Key: sk-..." \
  -d '{
    "preferred_provider": "ollama",
    "messages": [
      { "role": "user", "content": "¿Qué es Kubernetes?" }
    ]
  }'
```

El sistema automáticamente recuperará los chunks relevantes del documento ingestado y los usará para responder.

### Configuración del Sistema RAG

| Parámetro | Valor | Ubicación |
| :--- | :--- | :--- |
| **Modelo de Embeddings** | `nomic-embed-text` | Ollama |
| **Dimensionalidad** | 768 | `ingest_service.go:VectorSize` |
| **Tamaño de Chunk** | 500 caracteres | `ingest_service.go:ChunkSize` |
| **Top-K Retrieval** | 3 documentos | `chat_service.go:retrieveContext` |
| **Umbral de Relevancia** | Score > 0.25 | `chat_service.go` |
| **Colección Qdrant** | `knowledge_base` | `ingest_service.go:CollectionName` |

---

## 🕸 GraphRAG: Knowledge Graph

Además del RAG tradicional basado en vectores, el Gateway implementa **GraphRAG**: un sistema de extracción y búsqueda de conocimiento estructurado en forma de grafo.

### ¿Por qué GraphRAG?

El RAG vectorial es excelente para encontrar fragmentos de texto similares, pero pierde las **relaciones explícitas** entre conceptos. GraphRAG complementa esto extrayendo entidades y sus conexiones, permitiendo responder preguntas como:
- "¿Qué tecnologías usa el API Gateway?" → `api_gateway -[USES]-> redis, qdrant, neo4j`
- "¿Cómo se conecta curl con el sistema?" → `curl -[CONNECTS_TO]-> api_gateway`

### Flujo de Extracción (Durante Ingesta)

1. **Chunking:** El documento se divide en fragmentos (igual que Vector RAG).
2. **Extracción LLM:** Cada chunk se envía a Groq con un prompt especializado que extrae:
   - **Nodos:** Entidades con ID normalizado, nombre original y label (TOOL, CONCEPT, PERSON, ORG, PROJECT).
   - **Relaciones:** Conexiones tipadas entre entidades (USES, IMPLEMENTS, CONNECTS_TO, GENERATES, etc.).
3. **Normalización:** Los IDs se normalizan a lowercase con underscores (`API Gateway` → `api_gateway`).
4. **Keywords:** Se extraen palabras clave del nombre original para búsqueda flexible (`algoritmo_de_resumen` → `[algoritmo, de, resumen]`).
5. **Almacenamiento:** Nodos y relaciones se guardan en Neo4j usando Cypher MERGE (evita duplicados).

### Flujo de Búsqueda (Durante Chat)

1. **Tokenización:** La pregunta del usuario se tokeniza eliminando stopwords en español e inglés.
   - `"¿Qué tecnologías usa el algoritmo de resumen?"` → `[tecnologias, algoritmo, resumen]`
2. **Búsqueda Flexible:** Se buscan nodos en Neo4j que coincidan por:
   - ID normalizado contenido en el prompt
   - Nombre original contenido en el prompt
   - Palabras del query que coincidan con keywords del nodo
3. **Expansión:** Para cada entidad encontrada, se traen sus relaciones directas (1 salto).
4. **Inyección:** El contexto del grafo se añade al prompt junto con el contexto vectorial.

### Ejemplo de Contexto Generado

```
RELACIONES DEL GRAFO DE CONOCIMIENTO:
- api_gateway -[USES]-> redis
- api_gateway -[USES]-> qdrant
- api_gateway -[USES]-> neo4j
- api_gateway -[IMPLEMENTA]-> algoritmo_de_resumen
- ollama -[GENERATES]-> embeddings
- curl -[CONNECTS_TO]-> api_gateway
```

### Configuración de GraphRAG

| Parámetro | Valor | Ubicación |
| :--- | :--- | :--- |
| **LLM Extractor** | Groq (llama3) | `ingest_service.go` |
| **Max Nodos/Chunk** | 10 | `GraphSystemPrompt` |
| **Max Relaciones/Chunk** | 15 | `GraphSystemPrompt` |
| **Labels Soportados** | TOOL, CONCEPT, PERSON, ORG, PROJECT | `GraphSystemPrompt` |
| **Relaciones Comunes** | USES, IMPLEMENTS, DEPLOYS, CONNECTS_TO, IS_A, CREATED_BY, GENERATES | `GraphSystemPrompt` |
| **Min Keyword Length** | 2 caracteres | `extractKeywords()` |
| **Min Query Word Length** | 3 caracteres | `tokenizeQuery()` |
| **Stopwords** | ES + EN (el, la, the, is, que, como, etc.) | `tokenizeQuery()` |

### Soft Fail

Si la extracción de grafo falla (error de LLM, JSON inválido, etc.), el sistema **no falla el job completo**. Solo se loguea el error y se continúa con el siguiente chunk. Esto garantiza que el Vector RAG siempre funcione, incluso si GraphRAG tiene problemas temporales.

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
