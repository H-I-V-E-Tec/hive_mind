package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

// This test accepts only the isolated service ports used by the existing real
// ingestion test. Python runs Scout against a synthetic platform provider.
func TestScoutScopeDeliveryRealServices(t *testing.T) {
	if os.Getenv("HIVE_TEST_REAL_INGEST") != "1" || os.Getenv("HIVE_TEST_SCOUT_SOURCE") == "" {
		t.Skip("requires disposable Qdrant/Ollama and HIVE_TEST_SCOUT_SOURCE")
	}
	python := os.Getenv("HIVE_TEST_SCOUT_PYTHON")
	if python == "" {
		python = "python3"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	root := t.TempDir()
	cfg := defaultConversionLimits()
	cfg.HiveID, cfg.DeviceID, cfg.WriterApprovalID, cfg.Role = "scout-test", "synthetic-writer", "synthetic-approval", RoleWriter
	cfg.CollectionName = fmt.Sprintf("hive_remote_test_scout_%d", time.Now().UnixNano())
	cfg.ControlCollection = cfg.CollectionName + "__control"
	cfg.DataDirectory, cfg.WatchDirectory, cfg.AuditDirectory = root, root, filepath.Join(t.TempDir(), "audit")
	cfg.QdrantHost, cfg.QdrantPort, cfg.QdrantAPIKey = "127.0.0.1", 26334, newSecret("remote-ingest-test-only")
	cfg.OllamaHost, cfg.EmbeddingModel, cfg.MaxClassification, cfg.SearchMode = "http://127.0.0.1:21434", "all-minilm:latest", "restricted", "dense"
	cfg.MaxEmbeddingWorkers, cfg.BatchSize, cfg.BatchTimeout = 1, 100, time.Hour
	client, err := newQdrantClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	worker := NewIngestionWorker(cfg, client, nil)
	defer worker.Close()
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		client.DeleteCollection(cleanup, cfg.CollectionName)
		client.DeleteCollection(cleanup, cfg.ControlCollection)
	}()
	worker.Audit, err = OpenFileAudit(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Audit.(*FileAudit).Close()
	if err := worker.EnsureInfrastructure(ctx); err != nil {
		t.Fatal(err)
	}
	key := testRSAKey(t)
	issuer := testJWKSServer(t, testJWKS(t, "scout-key", &key.PublicKey))
	srv := NewHTTPServer(worker, NewJWKSClient(issuer.URL), ":0")
	mind := httptest.NewServer(srv.Handler())
	defer mind.Close()
	// Forward the first request completely, then lose only its acknowledgement.
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request, err := http.NewRequestWithContext(r.Context(), r.Method, mind.URL+r.URL.Path, r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		request.Header = r.Header.Clone()
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Error(err)
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if requests.Add(1) == 1 {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				connection.Close()
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.StatusCode)
		w.Write(body)
	}))
	defer proxy.Close()
	tokenFile := filepath.Join(t.TempDir(), "publisher.jwt")
	if err := os.WriteFile(tokenFile, []byte(memberToken(t, srv, key, "scout-key", []string{permissionMindRead, permissionScopePublish}, nil)), 0600); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(t.TempDir(), "state")
	probe := func(host string) map[string]any {
		t.Helper()
		script := filepath.Join(os.Getenv("HIVE_TEST_SCOUT_SOURCE"), "tests", "mind_delivery_probe.py")
		command := exec.CommandContext(ctx, python, script, "--url", proxy.URL, "--token-file", tokenFile, "--state-dir", stateDir, "--host", host)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("Scout process failed: %v: %s", err, output)
		}
		var result map[string]any
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("Scout did not return its result: %s", output)
		}
		return result
	}
	first := probe("scout.example.test")
	if first["status"] != "failed" || first["delivered"] != false {
		t.Fatalf("lost acknowledgement was reported as success: %+v", first)
	}
	manifestPath := filepath.Join(root, "programs", "teste-remoto", "scope.json")
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal("Scout did not reach Mind:", err)
	}
	exact := true
	count, err := client.Count(ctx, &qdrant.CountPoints{CollectionName: cfg.CollectionName, Exact: &exact})
	if err != nil || count == 0 {
		t.Fatalf("Scout scope was not indexed in real Qdrant: %d, %v", count, err)
	}
	before := worker.SnapshotEmbeddingStats().Calls
	replay := probe("scout.example.test")
	if replay["status"] != "success" || replay["manifest_sha256"] != sha256Hex(content) {
		t.Fatalf("exact-byte reconciliation failed: %+v", replay)
	}
	after, err := client.Count(ctx, &qdrant.CountPoints{CollectionName: cfg.CollectionName, Exact: &exact})
	if err != nil || after != count || worker.SnapshotEmbeddingStats().Calls != before {
		t.Fatal("replay added chunks or embedding calls")
	}
	if unchanged := probe("scout.example.test"); unchanged["status"] != "success" || requests.Load() != 2 {
		t.Fatalf("unchanged poll republished a timestamp: %+v", unchanged)
	}
	status, err := worker.ScopeStatus(ctx, "teste-remoto")
	if err != nil || status.Status != "unapproved" {
		t.Fatal("publication implicitly approved a scope")
	}
	if changed := probe("new-scout.example.test"); changed["status"] != "success" || changed["delivered"] != true {
		t.Fatalf("changed scope was not published: %+v", changed)
	}
	content, err = os.ReadFile(manifestPath)
	if err != nil || !strings.Contains(string(content), "new-scout.example.test") {
		t.Fatal("changed scope did not reach the canonical manifest")
	}
	status, err = worker.ScopeStatus(ctx, "teste-remoto")
	if err != nil || status.Status != "unapproved" {
		t.Fatal("changed scope was automatically approved")
	}
	t.Log("Scout -> HTTP/JWT -> real Ollama/Qdrant passed; lost response reconciled without duplicates; approval remains separate")
}
