package domain

// VectorDocument representa un fragmento de texto procesado
type VectorDocument struct {
	ID       string                 `json:"id"`
	Content  string                 `json:"content"`
	Metadata map[string]interface{} `json:"metadata"`
	Vector   []float32              `json:"vector,omitempty"`
}

// SearchResult es lo que recupera de la base
type SearchResult struct {
	Document VectorDocument
	Score    float32
}
