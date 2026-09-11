package server

import (
	"context"
	"strings"
	"testing"

	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type permissionQdrant struct {
	*memoryQdrant
	writeErr error
}

func (q *permissionQdrant) Upsert(ctx context.Context, in *qdrant.UpsertPoints) (*qdrant.UpdateResult, error) {
	if q.writeErr != nil {
		return nil, q.writeErr
	}
	return q.memoryQdrant.Upsert(ctx, in)
}

func TestSpec006CredentialCapabilitiesMatchRole(t *testing.T) {
	q := newMemoryQdrant()
	writer, _ := specWorker(t, q)
	if err := writer.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := writer.ValidateCredentialCapabilities(context.Background()); err != nil {
		t.Fatalf("writer read-write capability was rejected: %v", err)
	}

	reader := &IngestionWorker{Cfg: writer.Cfg, QdrantClient: &permissionQdrant{memoryQdrant: q, writeErr: status.Error(codes.PermissionDenied, "synthetic denial")}}
	reader.Cfg.Role = RoleReader
	if err := reader.ValidateCredentialCapabilities(context.Background()); err != nil {
		t.Fatalf("read-only credential was rejected: %v", err)
	}

	reader.QdrantClient = &permissionQdrant{memoryQdrant: q}
	if err := reader.ValidateCredentialCapabilities(context.Background()); err == nil || err.Error() != "Qdrant reader credential permits writes" {
		t.Fatalf("write-capable reader credential was accepted: %v", err)
	}
}

func TestSpec006CredentialFailuresAreFailClosedAndSanitized(t *testing.T) {
	q := newMemoryQdrant()
	q.collections["hive_data"] = true
	q.collections["hive_data__control"] = true
	q.points["hive_data"] = map[string]*qdrant.PointStruct{}
	q.points["hive_data__control"] = map[string]*qdrant.PointStruct{}
	worker := &IngestionWorker{Cfg: Config{HiveID: "test-hive", DeviceID: "reader-1", Role: RoleReader, CollectionName: "hive_data", ControlCollection: "hive_data__control"}}
	secretDetail := "synthetic-secret-must-not-leak"
	worker.QdrantClient = &permissionQdrant{memoryQdrant: q, writeErr: status.Error(codes.Unauthenticated, secretDetail)}
	err := worker.ValidateCredentialCapabilities(context.Background())
	if err == nil || strings.Contains(err.Error(), secretDetail) {
		t.Fatalf("authentication failure was not sanitized: %v", err)
	}
}
