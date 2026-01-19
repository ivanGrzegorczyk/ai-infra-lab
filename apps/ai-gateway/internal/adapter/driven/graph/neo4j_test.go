package graph

import (
	"context"
	"testing"
)

// TestNeo4jAdapter_HealthCheck prueba la conectividad básica
func TestNeo4jAdapter_HealthCheck(t *testing.T) {
	// Skip si no hay Neo4j disponible en el entorno de testing
	t.Skip("Skipping integration test - requires Neo4j instance")

	adapter, err := NewNeo4jAdapter("bolt://localhost:7687", "neo4j", "password")
	if err != nil {
		t.Fatalf("Failed to create Neo4j adapter: %v", err)
	}
	defer adapter.Close()

	ctx := context.Background()
	if err := adapter.HealthCheck(ctx); err != nil {
		t.Errorf("HealthCheck failed: %v", err)
	}
}

// TestNeo4jAdapter_ExecuteQuery prueba una consulta simple
func TestNeo4jAdapter_ExecuteQuery(t *testing.T) {
	t.Skip("Skipping integration test - requires Neo4j instance")

	adapter, err := NewNeo4jAdapter("bolt://localhost:7687", "neo4j", "password")
	if err != nil {
		t.Fatalf("Failed to create Neo4j adapter: %v", err)
	}
	defer adapter.Close()

	ctx := context.Background()

	// Crear un nodo de prueba
	err = adapter.ExecuteWrite(ctx, "CREATE (n:Test {name: $name})", map[string]interface{}{
		"name": "test-node",
	})
	if err != nil {
		t.Fatalf("Failed to create test node: %v", err)
	}

	// Consultar el nodo
	results, err := adapter.ExecuteQuery(ctx, "MATCH (n:Test {name: $name}) RETURN n.name as name", map[string]interface{}{
		"name": "test-node",
	})
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	// Limpiar
	err = adapter.ExecuteWrite(ctx, "MATCH (n:Test {name: $name}) DELETE n", map[string]interface{}{
		"name": "test-node",
	})
	if err != nil {
		t.Errorf("Failed to cleanup: %v", err)
	}
}

// TestNeo4jAdapter_ExecuteWrite prueba una operación de escritura
func TestNeo4jAdapter_ExecuteWrite(t *testing.T) {
	t.Skip("Skipping integration test - requires Neo4j instance")

	adapter, err := NewNeo4jAdapter("bolt://localhost:7687", "neo4j", "password")
	if err != nil {
		t.Fatalf("Failed to create Neo4j adapter: %v", err)
	}
	defer adapter.Close()

	ctx := context.Background()

	// Crear y eliminar un nodo de prueba
	err = adapter.ExecuteWrite(ctx, "CREATE (n:TempTest {id: $id})", map[string]interface{}{
		"id": "temp-123",
	})
	if err != nil {
		t.Errorf("Failed to execute write: %v", err)
	}

	// Verificar que existe
	results, err := adapter.ExecuteQuery(ctx, "MATCH (n:TempTest {id: $id}) RETURN n", map[string]interface{}{
		"id": "temp-123",
	})
	if err != nil {
		t.Fatalf("Failed to verify write: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected node to exist after write")
	}

	// Limpiar
	adapter.ExecuteWrite(ctx, "MATCH (n:TempTest {id: $id}) DELETE n", map[string]interface{}{
		"id": "temp-123",
	})
}
