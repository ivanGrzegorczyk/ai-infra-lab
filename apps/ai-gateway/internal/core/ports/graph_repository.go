package ports

import "context"

// GraphRepository define las operaciones básicas para interactuar con Neo4j
type GraphRepository interface {
	// Close cierra la conexión con Neo4j
	Close() error

	// ExecuteQuery ejecuta una consulta Cypher y devuelve los resultados
	ExecuteQuery(ctx context.Context, query string, params map[string]interface{}) ([]map[string]interface{}, error)

	// ExecuteWrite ejecuta una transacción de escritura
	ExecuteWrite(ctx context.Context, query string, params map[string]interface{}) error

	// HealthCheck verifica la conectividad con Neo4j
	HealthCheck(ctx context.Context) error
}
