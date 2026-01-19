package graph

import (
	"context"
	"fmt"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/ports"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type Neo4jAdapter struct {
	driver neo4j.DriverWithContext
}

// NewNeo4jAdapter crea una nueva instancia del adapter de Neo4j
func NewNeo4jAdapter(uri, username, password string) (*Neo4jAdapter, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return nil, fmt.Errorf("failed to create neo4j driver: %w", err)
	}

	// Verificar conectividad
	ctx := context.Background()
	if err := driver.VerifyConnectivity(ctx); err != nil {
		driver.Close(ctx)
		return nil, fmt.Errorf("failed to verify neo4j connectivity: %w", err)
	}

	return &Neo4jAdapter{
		driver: driver,
	}, nil
}

// Close cierra la conexión con Neo4j
func (a *Neo4jAdapter) Close() error {
	ctx := context.Background()
	return a.driver.Close(ctx)
}

// ExecuteQuery ejecuta una consulta Cypher de lectura y devuelve los resultados
func (a *Neo4jAdapter) ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error) {
	session := a.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeRead})
	defer session.Close(ctx)

	result, err := session.Run(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	var records []map[string]interface{}
	for result.Next(ctx) {
		record := result.Record()
		recordMap := make(map[string]interface{})

		for _, key := range record.Keys {
			value, ok := record.Get(key)
			if ok {
				recordMap[key] = value
			}
		}
		records = append(records, recordMap)
	}

	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("error iterating results: %w", err)
	}

	return records, nil
}

// ExecuteWrite ejecuta una transacción de escritura
func (a *Neo4jAdapter) ExecuteWrite(ctx context.Context, query string, params map[string]interface{}) error {
	session := a.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (interface{}, error) {
		result, err := tx.Run(ctx, query, params)
		if err != nil {
			return nil, err
		}

		// Consumir el resultado para asegurar que se ejecutó
		if err := result.Err(); err != nil {
			return nil, err
		}

		return nil, nil
	})

	if err != nil {
		return fmt.Errorf("failed to execute write: %w", err)
	}

	return nil
}

// HealthCheck verifica la conectividad con Neo4j
func (a *Neo4jAdapter) HealthCheck(ctx context.Context) error {
	return a.driver.VerifyConnectivity(ctx)
}

// Asegurar que Neo4jAdapter implementa la interfaz GraphRepository
var _ ports.GraphRepository = (*Neo4jAdapter)(nil)
