package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/adapters/clients"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/adapters/config"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/services"
	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/adapters/handlers" // Lo crearemos ahora
)

func main() {
	// Cargar Configuración
	cfg := config.Load()

	// Instanciar Adaptadores de Salida
	ollamaClient := clients.NewOllamaClient(cfg.OllamaURL)

	// Instanciar Lógica de Negocio e inyectar el cliente
	chatService := services.NewChatService(ollamaClient)

	// Instanciar Adaptadores de Entrada e inyectar el servicio
	chatHandler := handlers.NewChatHandler(chatService)

	// Configurar Rutas
	http.HandleFunc("/v1/chat", chatHandler.Handle)

	// Arrancar el Servidor
	fmt.Printf("AI Gateway corriendo en el puerto %s...\n", cfg.Port)
	fmt.Printf("Conectado a Ollama en: %s\n", cfg.OllamaURL)
	
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}
