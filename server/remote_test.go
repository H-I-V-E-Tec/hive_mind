package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func testRemoteServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "test"})
	})

	mux.HandleFunc("POST /api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing token"})
			return
		}
		var args HiveSearchArguments
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil || args.ProgramID == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "program_id is required"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HiveSearchResponse{
			Results:   []HiveSearchResult{{Text: "test result", Score: 0.9, Path: "doc.json", DocumentType: "note", EffectiveScopeStatus: "authorized", Classification: "internal", UntrustedContent: true}},
			Warnings:  []string{},
			Truncated: false,
		})
	})

	mux.HandleFunc("POST /api/v1/context", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing token"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HiveContextResponse{
			ProgramID: "acme",
			Asset:     ContextAsset{Type: "host", Value: "api.example.com"},
			Scope:     ContextScope{Status: "authorized", Confirmed: true, ActionAllowed: true},
			Warnings:  []string{},
			Items:     []HiveSearchResult{},
			Truncated: false,
		})
	})

	mux.HandleFunc("POST /api/v1/targets", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing token"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HiveListTargetsResponse{
			ProgramID: "acme", Targets: []TargetCatalogEntry{},
			Unconfirmed: []TargetCatalogEntry{}, Warnings: []string{}, Truncated: false,
		})
	})

	mux.HandleFunc("GET /api/v1/sync-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing token"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SyncStatusSnapshot{Status: "idle", PendingFiles: 0, ActiveSyncs: 0, TotalSynced: 42})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func setupTestToken(t *testing.T) {
	t.Helper()
	tmpHome := t.TempDir()
	setTestHome(t, tmpHome)

	dir := filepath.Join(tmpHome, ".hive")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "token"), []byte("test-jwt-token\n"), 0o600)
}

func TestRemoteSearchSuccess(t *testing.T) {
	srv := testRemoteServer(t)
	setupTestToken(t)

	rc := NewRemoteClient(srv.URL)
	resp, err := rc.HiveSearch(context.Background(), HiveSearchArguments{
		ProgramID: "acme", Query: "test query",
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Text != "test result" {
		t.Fatalf("unexpected results: %+v", resp)
	}
}

func TestRemoteSearchNoToken(t *testing.T) {
	srv := testRemoteServer(t)
	setTestHome(t, t.TempDir())

	rc := NewRemoteClient(srv.URL)
	_, err := rc.HiveSearch(context.Background(), HiveSearchArguments{
		ProgramID: "acme", Query: "test",
	})
	if err == nil {
		t.Fatal("expected error when no token stored")
	}
}

func TestRemoteSearchBadRequest(t *testing.T) {
	srv := testRemoteServer(t)
	setupTestToken(t)

	rc := NewRemoteClient(srv.URL)
	_, err := rc.HiveSearch(context.Background(), HiveSearchArguments{Query: "test"})
	if err == nil {
		t.Fatal("expected error for missing program_id")
	}
	if !isSearchValidationError(err) {
		t.Fatalf("expected validation error, got: %v", err)
	}
}

func TestRemoteContextSuccess(t *testing.T) {
	srv := testRemoteServer(t)
	setupTestToken(t)

	rc := NewRemoteClient(srv.URL)
	resp, err := rc.HiveGetContext(context.Background(), HiveContextArguments{
		ProgramID: "acme", Question: "what is this?",
		Asset: ContextAsset{Type: "host", Value: "api.example.com"},
	})
	if err != nil {
		t.Fatalf("context failed: %v", err)
	}
	if resp.Scope.Status != "authorized" {
		t.Fatalf("unexpected scope: %+v", resp.Scope)
	}
}

func TestRemoteTargetsSuccess(t *testing.T) {
	srv := testRemoteServer(t)
	setupTestToken(t)

	rc := NewRemoteClient(srv.URL)
	resp, err := rc.HiveListTargets(context.Background(), HiveListTargetsArguments{ProgramID: "acme"})
	if err != nil {
		t.Fatalf("targets failed: %v", err)
	}
	if resp.ProgramID != "acme" {
		t.Fatalf("unexpected program_id: %s", resp.ProgramID)
	}
}

func TestRemoteSyncStatus(t *testing.T) {
	srv := testRemoteServer(t)
	setupTestToken(t)

	rc := NewRemoteClient(srv.URL)
	snap, err := rc.SyncStatus()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != "idle" {
		t.Fatalf("expected idle, got %s", snap.Status)
	}
	if snap.TotalSynced != 42 {
		t.Fatalf("expected total_synced=42, got %d", snap.TotalSynced)
	}
}

func TestRemoteSyncStatusReportsAuthenticationFailure(t *testing.T) {
	srv := testRemoteServer(t)
	setTestHome(t, t.TempDir())
	_, err := NewRemoteClient(srv.URL).SyncStatus()
	if err == nil || syncStatusErrorMessage(err) != "Hive session is missing or expired; run 'hive login' and reconnect the MCP" {
		t.Fatalf("missing token was not reported: %v", err)
	}
}

func TestMCPRemoteSyncStatusReturnsErrorInsteadOfUnknownCounters(t *testing.T) {
	srv := testRemoteServer(t)
	setTestHome(t, t.TempDir())
	reply := remoteMCPReply(t, NewRemoteClient(srv.URL), "tools/call", CallToolParams{
		Name: "get_sync_status", Arguments: json.RawMessage(`{}`),
	})
	if len(reply["result"]) != 0 {
		t.Fatalf("status failure looked like a successful snapshot: %v", reply)
	}
	var failure struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(reply["error"], &failure); err != nil || failure.Code != -32603 ||
		failure.Message != "Hive session is missing or expired; run 'hive login' and reconnect the MCP" {
		t.Fatalf("unexpected MCP status failure: %v (%v)", reply, err)
	}
}

func TestRemoteBackendInterface(t *testing.T) {
	var _ HiveBackend = (*RemoteClient)(nil)
}
