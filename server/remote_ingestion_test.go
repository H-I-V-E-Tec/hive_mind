package server

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func remoteNote() HiveIngestDocumentArguments {
	return HiveIngestDocumentArguments{ProgramID: "teste-remoto", Classification: "internal", DocumentType: "note", SourceFormat: "txt", Content: "farol-violeta-427: nota sintética para verificar ingestão e busca remotas."}
}

func memberToken(t *testing.T, srv *HTTPServer, key *rsa.PrivateKey, kid string, permissions []string, overrides jwt.MapClaims) string {
	t.Helper()
	claims := jwt.MapClaims{"iss": srv.jwks.centerURL, "sub": "member-1", "aud": "mind", "exp": time.Now().Add(time.Hour).Unix(), "permissions": permissions}
	for k, v := range overrides {
		claims[k] = v
	}
	return signTestToken(t, key, kid, claims)
}

func documentRequest(srv *HTTPServer, token string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/documents/ingest", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func TestRemoteDocumentAuthorizationAndValidation(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	body, _ := json.Marshal(remoteNote())
	for _, tc := range []struct {
		name        string
		permissions []string
		overrides   jwt.MapClaims
		status      int
	}{
		{"reader", []string{permissionMindRead}, nil, 403},
		{"orphan ingestion", []string{permissionMindIngest}, nil, 403},
		{"no grants", nil, nil, 403},
		{"expired", []string{permissionMindRead, permissionMindIngest}, jwt.MapClaims{"exp": time.Now().Add(-time.Minute).Unix()}, 401},
		{"issuer", []string{permissionMindRead, permissionMindIngest}, jwt.MapClaims{"iss": "https://other.example.test"}, 401},
		{"audience", []string{permissionMindRead, permissionMindIngest}, jwt.MapClaims{"aud": "atlas"}, 401},
		{"no subject", []string{permissionMindRead, permissionMindIngest}, jwt.MapClaims{"sub": ""}, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := documentRequest(srv, memberToken(t, srv, key, kid, tc.permissions, tc.overrides), body)
			if rec.Code != tc.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
	token := memberToken(t, srv, key, kid, []string{permissionMindRead, permissionMindIngest}, nil)
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"path", `{"program_id":"../outside","classification":"internal","document_type":"note","source_format":"txt","content":"test"}`, 400},
		{"author", `{"program_id":"test","classification":"internal","document_type":"note","source_format":"txt","content":"test","author":"other-member"}`, 400},
		{"empty", `{"program_id":"test","classification":"internal","document_type":"note","source_format":"txt","content":" "}`, 400},
		{"binary", `{"program_id":"test","classification":"internal","document_type":"note","source_format":"txt","content":"a\u0000b"}`, 400},
		{"scope", `{"program_id":"test","classification":"internal","document_type":"scope","source_format":"txt","content":"test"}`, 400},
		{"trailing", string(body) + ` {}`, 400},
		{"trailing body limit", string(body) + strings.Repeat(" ", remoteRequestLimit), 413},
		{"invalid UTF-8", `{"program_id":"test","classification":"internal","document_type":"note","source_format":"txt","content":"` + string([]byte{0xff}) + `"}`, 400},
		{"body limit", strings.Repeat(" ", remoteRequestLimit+1), 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := documentRequest(srv, token, []byte(tc.body))
			if rec.Code != tc.status {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
	oversized := remoteNote()
	oversized.Content = strings.Repeat("á", remoteContentLimit/2+1)
	tooBig, _ := json.Marshal(oversized)
	if rec := documentRequest(srv, token, tooBig); rec.Code != 413 {
		t.Fatalf("byte limit status %d", rec.Code)
	}
	files, _ := filepath.Glob(filepath.Join(srv.worker.Cfg.DataDirectory, "programs", "*", "imports", "remote", "*", "*.json"))
	if len(files) != 0 {
		t.Fatal("denied or invalid requests wrote documents")
	}
	for _, path := range []string{"/api/v1/search", "/api/v1/ingest"} {
		grants := []string{}
		if strings.HasSuffix(path, "/ingest") {
			grants = []string{permissionMindRead}
		}
		req := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+memberToken(t, srv, key, kid, grants, nil))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("%s: status %d", path, rec.Code)
		}
	}
	srv.worker.Cfg.Role = RoleReader
	if rec := documentRequest(srv, token, body); rec.Code != 403 {
		t.Fatalf("reader instance: status %d", rec.Code)
	}
}

func TestRemoteScopeRequiresDedicatedPermissionAndSeparateApproval(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	manifest := `{"schema_version":1,"program_id":"remote-scope","platform":"h1","target_name":"@program","classification":"internal","source":"synthetic portal","collected_at":"2026-10-07T12:00:00Z","rules":[{"action":"include","asset_type":"host","value":"api.example.test"}]}`
	args := HiveIngestDocumentArguments{ProgramID: "remote-scope", Classification: "internal",
		DocumentType: "scope", SourceFormat: "json", Content: manifest}
	body, _ := json.Marshal(args)

	ordinary := memberToken(t, srv, key, kid, []string{permissionMindRead, permissionMindIngest}, nil)
	if rec := documentRequest(srv, ordinary, body); rec.Code != http.StatusForbidden {
		t.Fatalf("ordinary ingestion published scope: %d %s", rec.Code, rec.Body.String())
	}

	scopeToken := memberToken(t, srv, key, kid, []string{permissionMindRead, permissionScopeAdmin}, nil)
	if rec := documentRequest(srv, scopeToken, body); rec.Code != http.StatusOK {
		t.Fatalf("scope publication failed: %d %s", rec.Code, rec.Body.String())
	}
	revision, _, err := srv.worker.resolveActiveScope(context.Background(), "remote-scope")
	if err != nil || revision != "unapproved" {
		t.Fatalf("publication implicitly approved scope: revision=%s err=%v", revision, err)
	}

	approveBody, _ := json.Marshal(map[string]string{"sha256": sha256Hex([]byte(manifest))})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scopes/remote-scope/approve", bytes.NewReader(approveBody))
	req.Header.Set("Authorization", "Bearer "+scopeToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("scope approval failed: %d %s", rec.Code, rec.Body.String())
	}
	revision, _, err = srv.worker.resolveActiveScope(context.Background(), "remote-scope")
	if err != nil || revision != sha256Hex([]byte(manifest)) {
		t.Fatalf("scope was not approved: revision=%s err=%v", revision, err)
	}
}

func TestRemoteDocumentPublicationReplayAndSearch(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	token := memberToken(t, srv, key, kid, []string{permissionMindRead, permissionMindIngest}, nil)
	body, _ := json.Marshal(remoteNote())
	created := documentRequest(srv, token, body)
	var report IngestionReport
	if err := json.Unmarshal(created.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if created.Code != 200 || !report.OK || report.Summary.Created != 1 || !report.Summary.Results[0].Published {
		t.Fatalf("creation: %d %+v", created.Code, report)
	}
	path := report.Summary.Results[0].Path
	content, err := os.ReadFile(filepath.Join(srv.worker.Cfg.DataDirectory, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	var doc ConvertedDocument
	json.Unmarshal(content, &doc)
	if doc.Source != "remote-member:member-1" || doc.ProgramID != remoteNote().ProgramID {
		t.Fatalf("provenance: %+v", doc)
	}
	before := srv.worker.SnapshotEmbeddingStats().Calls
	replay := documentRequest(srv, token, body)
	json.Unmarshal(replay.Body.Bytes(), &report)
	if replay.Code != 200 || !report.OK || report.Summary.Unchanged != 1 || srv.worker.SnapshotEmbeddingStats().Calls != before {
		t.Fatalf("replay: %+v", report)
	}
	srv.worker.QdrantClient = &scoringQdrant{srv.worker.QdrantClient.(*memoryQdrant)}
	response, err := srv.worker.HiveSearch(context.Background(), HiveSearchArguments{ProgramID: remoteNote().ProgramID, Query: "farol-violeta-427", EffectiveScopeStatus: "unknown"})
	if err != nil || len(response.Results) == 0 || !strings.Contains(response.Results[0].Text, "farol-violeta-427") {
		t.Fatalf("search: %+v, %v", response, err)
	}
	other := memberToken(t, srv, key, kid, []string{permissionMindRead, permissionMindIngest}, jwt.MapClaims{"sub": "member-2"})
	second := documentRequest(srv, other, body)
	json.Unmarshal(second.Body.Bytes(), &report)
	if !report.OK || report.Summary.Created != 1 || report.Summary.Results[0].Path == path {
		t.Fatalf("member namespace: %+v", report)
	}
}

func TestRemoteDocumentConcurrencyAndWatcherReuse(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	token := memberToken(t, srv, key, kid, []string{permissionMindRead, permissionMindIngest}, nil)
	body, _ := json.Marshal(remoteNote())
	var wg sync.WaitGroup
	beforeCalls := srv.worker.SnapshotEmbeddingStats().Calls
	reports := make(chan IngestionReport, 8)
	for range 8 {
		wg.Go(func() {
			rec := documentRequest(srv, token, body)
			var report IngestionReport
			json.Unmarshal(rec.Body.Bytes(), &report)
			reports <- report
		})
	}
	wg.Wait()
	close(reports)
	created := 0
	path := ""
	for report := range reports {
		if !report.OK {
			t.Fatalf("concurrent report: %+v", report)
		}
		created += report.Summary.Created
		path = report.Summary.Results[0].Path
	}
	if created != 1 || srv.worker.SnapshotEmbeddingStats().Calls-beforeCalls != 1 {
		t.Fatalf("created=%d embeddings=%d", created, srv.worker.SnapshotEmbeddingStats().Calls)
	}
	before := srv.worker.SnapshotEmbeddingStats().Calls
	if err := srv.worker.SyncFileState(context.Background(), filepath.Join(srv.worker.Cfg.DataDirectory, filepath.FromSlash(path))); err != nil {
		t.Fatal(err)
	}
	if srv.worker.SnapshotEmbeddingStats().Calls != before {
		t.Fatal("watcher re-embedded the active document")
	}
}

func TestRemoteDocumentSymlinkAndCollision(t *testing.T) {
	for _, internal := range []bool{false, true} {
		t.Run(map[bool]string{false: "outside", true: "other program"}[internal], func(t *testing.T) {
			worker, root := specWorker(t, newMemoryQdrant())
			outside := t.TempDir()
			if internal {
				outside = filepath.Join(root, "programs", "other")
				os.MkdirAll(outside, 0o700)
			}
			if err := os.Symlink(outside, filepath.Join(root, "programs", remoteNote().ProgramID)); err != nil {
				t.Skip(err)
			}
			report := worker.IngestRemoteDocument(context.Background(), &HiveClaims{Sub: "member-1", Permissions: []string{permissionMindRead, permissionMindIngest}}, remoteNote())
			if report.OK {
				t.Fatal("symlink accepted")
			}
			entries, _ := os.ReadDir(outside)
			if len(entries) != 0 {
				t.Fatal("symlink destination was written")
			}
		})
	}
	root := t.TempDir()
	rel := filepath.Join("programs", "test", "imports", "remote", "member", "note.json")
	if err := publishRemoteDocument(root, rel, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := publishRemoteDocument(root, rel, []byte("replaced")); err == nil {
		t.Fatal("collision overwrote content")
	}
	content, _ := os.ReadFile(filepath.Join(root, rel))
	if string(content) != "original" {
		t.Fatal("original changed")
	}
}

func TestRemoteDocumentFailureReportPreserved(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	srv.worker.HTTPClient.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(`{"error":"synthetic-private-provider-detail"}`)), Header: http.Header{}}, nil
	})
	setTestHome(t, t.TempDir())
	if err := storeToken(memberToken(t, srv, key, kid, []string{permissionMindRead, permissionMindIngest}, nil)); err != nil {
		t.Fatal(err)
	}
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	client := NewRemoteClient(httpSrv.URL)
	report := client.IngestDocument(context.Background(), remoteNote())
	if report.OK || report.Summary.Failed != 1 || len(report.Summary.Results) != 1 || report.Summary.Results[0].ReasonCode != "embedding_failed" {
		t.Fatalf("failure report: %+v", report)
	}
	encoded, _ := json.Marshal(ingestionMCPResponse(json.RawMessage(`1`), report))
	if !strings.Contains(string(encoded), `"isError":true`) || strings.Contains(string(encoded), "synthetic-private-provider-detail") {
		t.Fatalf("MCP failure: %s", encoded)
	}
}

// Exercise the same JSON response used by stdio, including asynchronous calls.
func remoteMCPReply(t *testing.T, backend HiveBackend, method string, params any) map[string]json.RawMessage {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = stdout; writer.Close(); reader.Close() }()
	// Start reading before synchronous responses fill the pipe. Pipe deadlines
	// are not portable, so bound the response wait with a Go timer instead.
	var reply map[string]json.RawMessage
	decoded := make(chan error, 1)
	go func() { decoded <- json.NewDecoder(reader).Decode(&reply) }()
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	encoded, _ := json.Marshal(params)
	handler := MCPHandler{backend: backend}
	handler.handleMCPMethod(MCPRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: method, Params: encoded})
	select {
	case err := <-decoded:
		if err != nil {
			t.Fatal(err)
		}
	case <-timer.C:
		t.Fatal("timed out waiting for the MCP response")
	}
	return reply
}

func remoteMCPReport(t *testing.T, backend HiveBackend, args HiveIngestDocumentArguments) IngestionReport {
	t.Helper()
	encoded, _ := json.Marshal(args)
	reply := remoteMCPReply(t, backend, "tools/call", CallToolParams{Name: "hive_ingest_document", Arguments: encoded})
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(reply["result"], &result); err != nil || len(result.Content) != 1 {
		t.Fatalf("MCP reply: %s; %v", reply, err)
	}
	var report IngestionReport
	if err := json.Unmarshal([]byte(result.Content[0].Text), &report); err != nil {
		t.Fatal(err)
	}
	if result.IsError == report.OK {
		t.Fatalf("MCP error flag disagrees with report: %+v", report)
	}
	return report
}

func TestRemoteMCPIngestionCapabilitiesAndExecution(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	setTestHome(t, t.TempDir())
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	client := NewRemoteClient(httpSrv.URL)
	for _, contributor := range []bool{false, true} {
		grants := []string{permissionMindRead}
		if contributor {
			grants = append(grants, permissionMindIngest)
		}
		if err := storeToken(memberToken(t, srv, key, kid, grants, nil)); err != nil {
			t.Fatal(err)
		}
		reply := remoteMCPReply(t, client, "tools/list", nil)
		var result struct {
			Tools []map[string]interface{} `json:"tools"`
		}
		json.Unmarshal(reply["result"], &result)
		if containsTool(result.Tools, "hive_ingest_document") != contributor || containsTool(result.Tools, "ingest_workspace") {
			t.Fatalf("remote tools: %+v", result.Tools)
		}
	}
	report := remoteMCPReport(t, client, remoteNote())
	if !report.OK || report.Summary.Created != 1 {
		t.Fatalf("MCP publication: %+v", report)
	}
	if err := storeToken(memberToken(t, srv, key, kid, []string{permissionMindRead, permissionMindIngest}, jwt.MapClaims{"exp": time.Now().Add(-time.Minute).Unix()})); err != nil {
		t.Fatal(err)
	}
	if client.CanIngestDocument() {
		t.Fatal("expired token exposed ingestion")
	}
	if (&workerBackend{worker: srv.worker}).CanIngestDocument() {
		t.Fatal("local workspace exposed remote member ingestion")
	}
}

func TestRemoteUnverifiedToolClaimsNeverAuthorizePublication(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	setTestHome(t, t.TempDir())
	token := memberToken(t, srv, key, kid, []string{permissionMindRead}, nil)
	parts := strings.Split(token, ".")
	payload, _ := decodeJWTPayload(parts[1])
	var claims map[string]any
	json.Unmarshal(payload, &claims)
	claims["permissions"] = []string{permissionMindRead, permissionMindIngest}
	changed, _ := json.Marshal(claims)
	parts[1] = base64.RawURLEncoding.EncodeToString(changed)
	if err := storeToken(strings.Join(parts, ".")); err != nil {
		t.Fatal(err)
	}
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	client := NewRemoteClient(httpSrv.URL)
	if !client.CanIngestDocument() {
		t.Fatal("fixture did not advertise unverified contribution")
	}
	report := client.IngestDocument(context.Background(), remoteNote())
	if report.OK || !strings.Contains(report.Error, "reauthenticate") {
		t.Fatalf("tampered claims authorized ingestion: %+v", report)
	}
	files, _ := filepath.Glob(filepath.Join(srv.worker.Cfg.DataDirectory, "programs", "*", "imports", "remote", "*", "*.json"))
	if len(files) != 0 {
		t.Fatal("tampered token wrote documents")
	}
}

func TestRemotePublicationSerializesWatcherAndHonorsWaitingDeadline(t *testing.T) {
	srv, _, _ := testHTTPServer(t)
	base := srv.worker.HTTPClient.Transport
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	srv.worker.HTTPClient.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		close(entered)
		select {
		case <-release:
			return base.RoundTrip(req)
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	})
	done := make(chan IngestionReport, 1)
	go func() {
		done <- srv.worker.IngestRemoteDocument(context.Background(), &HiveClaims{Sub: "member-1", Permissions: []string{permissionMindRead, permissionMindIngest}}, remoteNote())
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("embedding did not start")
	}
	files, _ := filepath.Glob(filepath.Join(srv.worker.Cfg.DataDirectory, "programs", "*", "imports", "remote", "*", "*.json"))
	if len(files) != 1 {
		t.Fatal("expected one complete source file")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := srv.worker.SyncFileState(ctx, files[0]); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("watcher waiting deadline: %v", err)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case report := <-done:
		if !report.OK || report.Summary.Created != 1 {
			t.Fatalf("publication: %+v", report)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("publication did not finish")
	}
}
