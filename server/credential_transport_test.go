package server

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type authenticatedPoints struct {
	qdrant.UnimplementedPointsServer
}

func (authenticatedPoints) Count(ctx context.Context, in *qdrant.CountPoints) (*qdrant.CountResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	if values := md.Get("api-key"); len(values) != 1 || values[0] != "synthetic-transport-token" {
		return nil, status.Error(codes.Unauthenticated, "synthetic denial")
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, status.Error(codes.InvalidArgument, "deadline missing")
	}
	return &qdrant.CountResponse{Result: &qdrant.CountResult{Count: 7}}, nil
}

func TestSpec006ActualClientForwardsAPIKeyAndDeadline(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	qdrant.RegisterPointsServer(server, authenticatedPoints{})
	go server.Serve(listener)
	defer server.Stop()
	defer listener.Close()
	client, err := newQdrantClientWithOptions(Config{QdrantHost: "127.0.0.1", QdrantAPIKey: newSecret("synthetic-transport-token")}, []grpc.DialOption{grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) })})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	count, err := client.Count(ctx, &qdrant.CountPoints{CollectionName: "hive_data"})
	if err != nil || count != 7 {
		t.Fatalf("authenticated gRPC call failed: %d %v", count, err)
	}
}

type perCollectionPermissions struct {
	*memoryQdrant
	denied map[string]bool
	calls  []string
}

func (p *perCollectionPermissions) Delete(ctx context.Context, in *qdrant.DeletePoints) (*qdrant.UpdateResult, error) {
	p.calls = append(p.calls, in.CollectionName)
	if p.denied[in.CollectionName] {
		return nil, status.Error(codes.PermissionDenied, "synthetic denial")
	}
	return p.memoryQdrant.Delete(ctx, in)
}

func TestSpec006RejectsMixedCollectionPermissions(t *testing.T) {
	for _, role := range []string{RoleReader, RoleWriter} {
		for _, denied := range []string{"hive_data", "hive_data__control"} {
			t.Run(role+"/"+denied, func(t *testing.T) {
				memory := newMemoryQdrant()
				w, _ := specWorker(t, memory)
				if err := w.EnsureInfrastructure(context.Background()); err != nil {
					t.Fatal(err)
				}
				p := &perCollectionPermissions{memoryQdrant: memory, denied: map[string]bool{denied: true}}
				w.Cfg.Role = role
				w.QdrantClient = p
				if err := w.ValidateCredentialCapabilities(context.Background()); err == nil {
					t.Fatal("mixed collection privileges accepted")
				}
			})
		}
	}
}

func TestSpec006NoMatchProbeNeverDeletesExistingPoints(t *testing.T) {
	memory := newMemoryQdrant()
	w, _ := specWorker(t, memory)
	if err := w.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatal(err)
	}
	point := &qdrant.PointStruct{Id: qdrant.NewIDUUID("00000000-0000-0000-0000-000000000001"), Payload: qdrant.NewValueMap(map[string]any{"record_type": "credential_probe"})}
	memory.points[w.Cfg.CollectionName][point.Id.GetUuid()] = point
	if err := w.ValidateCredentialCapabilities(context.Background()); err != nil {
		t.Fatal(err)
	}
	if memory.points[w.Cfg.CollectionName][point.Id.GetUuid()] != point {
		t.Fatal("permission probe deleted existing data")
	}
}
