package domain

import "time"

// Message representa un mensaje individual en una conversación de chat.
// Estándar de Role/Content para ser compatibles con la mayoría de los LLMs.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest es la estructura que el Gateway recibe del cliente.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature float32   `json:"temperature"`
}

// ChatResponse representa un fragmento de respuesta (token) o una respuesta completa.
// Esto sirve tanto para streaming como para respuestas síncronas.
type ChatResponse struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Provider  string    `json:"provider"` // Para saber si vino de Ollama, Groq u otro proveedor
	CreatedAt time.Time `json:"created_at"`
}
