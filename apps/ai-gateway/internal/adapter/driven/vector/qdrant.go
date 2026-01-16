package vector

import (
	"context"
	"fmt"

	"github.com/ivanGrzegorczyk/ai-infra-gateway/internal/core/domain"
	pb "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type QdrantAdapter struct {
	conn              *grpc.ClientConn
	collectionsClient pb.CollectionsClient // Cliente específico para gestionar Tablas
	pointsClient      pb.PointsClient      // Cliente específico para gestionar Datos (Vectores)
}

func NewQdrantAdapter(addr string) (*QdrantAdapter, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("fallo al conectar con qdrant: %w", err)
	}

	return &QdrantAdapter{
		conn:              conn,
		collectionsClient: pb.NewCollectionsClient(conn),
		pointsClient:      pb.NewPointsClient(conn),
	}, nil
}

func (q *QdrantAdapter) EnsureCollection(ctx context.Context, name string, vectorSize uint64) error {
	collections, err := q.collectionsClient.List(ctx, &pb.ListCollectionsRequest{})
	if err != nil {
		return fmt.Errorf("error listando colecciones: %w", err)
	}

	for _, col := range collections.Collections {
		if col.Name == name {
			return nil // Ya existe
		}
	}

	_, err = q.collectionsClient.Create(ctx, &pb.CreateCollection{
		CollectionName: name,
		VectorsConfig: &pb.VectorsConfig{
			Config: &pb.VectorsConfig_Params{
				Params: &pb.VectorParams{
					Size:     vectorSize,
					Distance: pb.Distance_Cosine,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("error creando colección %s: %w", name, err)
	}
	return nil
}

func (q *QdrantAdapter) Upsert(ctx context.Context, collectionName string, docs []domain.VectorDocument) error {
	points := make([]*pb.PointStruct, len(docs))

	for i, doc := range docs {
		payload := make(map[string]*pb.Value)
		payload["content"] = &pb.Value{Kind: &pb.Value_StringValue{StringValue: doc.Content}}

		for k, v := range doc.Metadata {
			if strVal, ok := v.(string); ok {
				payload[k] = &pb.Value{Kind: &pb.Value_StringValue{StringValue: strVal}}
			}
		}

		points[i] = &pb.PointStruct{
			Id:      &pb.PointId{PointIdOptions: &pb.PointId_Uuid{Uuid: doc.ID}},
			Vectors: &pb.Vectors{VectorsOptions: &pb.Vectors_Vector{Vector: &pb.Vector{Data: doc.Vector}}},
			Payload: payload,
		}
	}

	wait := true
	_, err := q.pointsClient.Upsert(ctx, &pb.UpsertPoints{
		CollectionName: collectionName,
		Points:         points,
		Wait:           &wait,
	})
	if err != nil {
		return fmt.Errorf("error en upsert: %w", err)
	}
	return nil
}

func (q *QdrantAdapter) Search(ctx context.Context, collectionName string, vector []float32, limit uint64) ([]domain.SearchResult, error) {
	res, err := q.pointsClient.Search(ctx, &pb.SearchPoints{
		CollectionName: collectionName,
		Vector:         vector,
		Limit:          limit,
		WithPayload:    &pb.WithPayloadSelector{SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true}},
	})
	if err != nil {
		return nil, fmt.Errorf("error en búsqueda vectorial: %w", err)
	}

	results := make([]domain.SearchResult, len(res.Result))
	for i, point := range res.Result {
		content := ""
		if val, ok := point.Payload["content"]; ok {
			content = val.GetStringValue()
		}

		results[i] = domain.SearchResult{
			Score: point.Score,
			Document: domain.VectorDocument{
				ID:      point.Id.GetUuid(),
				Content: content,
			},
		}
	}
	return results, nil
}

func (q *QdrantAdapter) Close() error {
	return q.conn.Close()
}
