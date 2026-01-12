package domain

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Messages          []ChatMessage `json:"messages"`
	PreferredProvider string        `json:"preferred_provider,omitempty"` // Opcional: "ollama" o "groq"
}

type ChatResponse struct {
	Content  string `json:"content"`
	Provider string `json:"provider"`
}
