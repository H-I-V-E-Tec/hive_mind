package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

// Opt-in, fixed isolated ports and synthetic key: never accept production URLs.
// See docs/spec/contracts/remote-ingestion.md for the disposable containers.
func TestRemoteIngestionRealServices(t *testing.T) {
	if os.Getenv("HIVE_TEST_REAL_INGEST") != "1" {
		t.Skip("requires disposable Qdrant :26334 and Ollama :21434 with all-minilm")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root := t.TempDir()
	collection := fmt.Sprintf("hive_remote_test_%d", time.Now().UnixNano())
	cfg := defaultConversionLimits()
	cfg.HiveID, cfg.DeviceID, cfg.WriterApprovalID, cfg.Role = "remote-test", "test-writer", "synthetic-test", RoleWriter
	cfg.CollectionName, cfg.ControlCollection = collection, collection+"__control"
	cfg.DataDirectory, cfg.WatchDirectory, cfg.AuditDirectory = root, root, filepath.Join(t.TempDir(), "audit")
	cfg.QdrantHost, cfg.QdrantPort, cfg.QdrantAPIKey = "127.0.0.1", 26334, newSecret("remote-ingest-test-only")
	cfg.OllamaHost, cfg.EmbeddingModel, cfg.MaxClassification, cfg.SearchMode = "http://127.0.0.1:21434", "all-minilm:latest", "internal", "dense"
	cfg.MaxEmbeddingWorkers, cfg.BatchSize, cfg.BatchTimeout = 1, 100, time.Hour
	client, err := newQdrantClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		client.DeleteCollection(cleanup, cfg.CollectionName)
		client.DeleteCollection(cleanup, cfg.ControlCollection)
	}()
	worker := NewIngestionWorker(cfg, client, nil)
	defer worker.Close()
	worker.Audit, err = OpenFileAudit(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Audit.(*FileAudit).Close()
	if err := worker.EnsureInfrastructure(ctx); err != nil {
		t.Fatal(err)
	}
	key := testRSAKey(t)
	kid := "real-service-test"
	issuer := testJWKSServer(t, testJWKS(t, kid, &key.PublicKey))
	srv := NewHTTPServer(worker, NewJWKSClient(issuer.URL), ":0")
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	setTestHome(t, t.TempDir())
	if err := storeToken(memberToken(t, srv, key, kid, []string{permissionMindRead, permissionMindIngest}, nil)); err != nil {
		t.Fatal(err)
	}
	remote := NewRemoteClient(httpSrv.URL)
	before := worker.SnapshotEmbeddingStats().Calls
	report := remoteMCPReport(t, remote, remoteNote())
	if !report.OK || report.Summary.Created != 1 || !report.Summary.Results[0].Published {
		t.Fatalf("publication: %+v", report)
	}
	exact := true
	count, err := client.Count(ctx, &qdrant.CountPoints{CollectionName: cfg.CollectionName, Exact: &exact})
	if err != nil || count != 1 {
		t.Fatalf("chunks: %d, %v", count, err)
	}
	replay := remoteMCPReport(t, remote, remoteNote())
	if !replay.OK || replay.Summary.Unchanged != 1 || worker.SnapshotEmbeddingStats().Calls-before != 1 {
		t.Fatalf("replay: %+v", replay)
	}
	after, err := client.Count(ctx, &qdrant.CountPoints{CollectionName: cfg.CollectionName, Exact: &exact})
	if err != nil || after != count {
		t.Fatalf("duplicate chunks: %d, %v", after, err)
	}
	response, err := remote.HiveSearch(ctx, HiveSearchArguments{ProgramID: remoteNote().ProgramID, Query: "farol-violeta-427", EffectiveScopeStatus: "unknown"})
	if err != nil || len(response.Results) != 1 || !strings.Contains(response.Results[0].Text, "farol-violeta-427") {
		t.Fatalf("real search: %+v, %v", response, err)
	}
	if response.Results[0].Source != "remote-member:member-1" || !response.Results[0].UntrustedContent {
		t.Fatal("missing provenance/untrusted flag")
	}
	if err := storeToken(memberToken(t, srv, key, kid, []string{permissionMindRead}, nil)); err != nil {
		t.Fatal(err)
	}
	denied := remote.IngestDocument(ctx, remoteNote())
	if denied.OK || !strings.Contains(denied.Error, "forbidden") {
		t.Fatalf("reader direct request: %+v", denied)
	}
	// Keep the actual HTTP client: every embedding, write, validation and query above is real.
	if worker.HTTPClient.Transport != nil || worker.HTTPClient.Timeout != 15*time.Second {
		t.Fatal("unexpected synthetic embedding transport")
	}
	t.Log("MCP -> HTTP/JWT -> real Ollama -> real Qdrant -> remote search passed; replay added no embeddings or chunks; reader denied")
}
