package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

type memoryQdrant struct {
	mu          sync.Mutex
	collections map[string]bool
	points      map[string]map[string]*qdrant.PointStruct
	events      []string
	upsertErr   error
	deleteErr   error
	countErr    error
	indexes     map[string]map[string]qdrant.FieldType
	queryCalls  []*qdrant.QueryPoints
	queryResp   []*qdrant.ScoredPoint
}

func newMemoryQdrant() *memoryQdrant {
	return &memoryQdrant{collections: map[string]bool{}, points: map[string]map[string]*qdrant.PointStruct{}, indexes: map[string]map[string]qdrant.FieldType{}}
}

func (m *memoryQdrant) Upsert(_ context.Context, in *qdrant.UpsertPoints) (*qdrant.UpdateResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !in.GetWait() {
		return nil, errors.New("write did not request wait=true")
	}
	if m.upsertErr != nil {
		return nil, m.upsertErr
	}
	// Match Qdrant's gRPC validation, including for vectorless collections.
	for _, point := range in.Points {
		if point.GetVectors().GetVectorsOptions() == nil {
			return nil, errors.New("Expected some vectors")
		}
	}
	if m.points[in.CollectionName] == nil {
		m.points[in.CollectionName] = map[string]*qdrant.PointStruct{}
	}
	for _, point := range in.Points {
		m.points[in.CollectionName][point.Id.GetUuid()] = point
	}
	kind := "chunks"
	if in.CollectionName == "hive_data__control" && len(in.Points) > 0 {
		kind = payloadString(in.Points[0].Payload, "record_type", "control")
	}
	m.events = append(m.events, "upsert:"+kind)
	return &qdrant.UpdateResult{Status: qdrant.UpdateStatus_Completed}, nil
}

func (m *memoryQdrant) Delete(_ context.Context, in *qdrant.DeletePoints) (*qdrant.UpdateResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !in.GetWait() {
		return nil, errors.New("delete did not request wait=true")
	}
	if m.deleteErr != nil {
		return nil, m.deleteErr
	}
	for id, point := range m.points[in.CollectionName] {
		if matchesFilter(point.Payload, in.Points.GetFilter()) {
			delete(m.points[in.CollectionName], id)
		}
	}
	m.events = append(m.events, "delete:chunks")
	return &qdrant.UpdateResult{Status: qdrant.UpdateStatus_Completed}, nil
}

func (m *memoryQdrant) CollectionExists(_ context.Context, name string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.collections[name], nil
}
func (m *memoryQdrant) CreateCollection(_ context.Context, in *qdrant.CreateCollection) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collections[in.CollectionName] = true
	m.points[in.CollectionName] = map[string]*qdrant.PointStruct{}
	m.events = append(m.events, "create:"+in.CollectionName)
	return nil
}
func (m *memoryQdrant) CreateFieldIndex(_ context.Context, in *qdrant.CreateFieldIndexCollection) (*qdrant.UpdateResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !in.GetWait() {
		return nil, errors.New("index creation did not request wait=true")
	}
	if m.indexes[in.CollectionName] == nil {
		m.indexes[in.CollectionName] = map[string]qdrant.FieldType{}
	}
	m.indexes[in.CollectionName][in.FieldName] = in.GetFieldType()
	return &qdrant.UpdateResult{Status: qdrant.UpdateStatus_Completed}, nil
}
func (m *memoryQdrant) Query(_ context.Context, in *qdrant.QueryPoints) ([]*qdrant.ScoredPoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queryCalls = append(m.queryCalls, in)
	return m.queryResp, nil
}
func (m *memoryQdrant) Scroll(_ context.Context, in *qdrant.ScrollPoints) ([]*qdrant.RetrievedPoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*qdrant.RetrievedPoint
	for _, point := range m.points[in.CollectionName] {
		if matchesFilter(point.Payload, in.Filter) {
			retrieved := &qdrant.RetrievedPoint{Id: point.Id, Payload: point.Payload}
			if in.GetWithVectors().GetEnable() {
				retrieved.Vectors = testVectorsOutput(point.Vectors)
			}
			out = append(out, retrieved)
		}
	}
	return out, nil
}

func testVectorsOutput(input *qdrant.Vectors) *qdrant.VectorsOutput {
	if input == nil {
		return nil
	}
	if vector := input.GetVector(); vector != nil {
		return &qdrant.VectorsOutput{VectorsOptions: &qdrant.VectorsOutput_Vector{Vector: testVectorOutput(vector)}}
	}
	named := map[string]*qdrant.VectorOutput{}
	for name, vector := range input.GetVectors().GetVectors() {
		named[name] = testVectorOutput(vector)
	}
	return &qdrant.VectorsOutput{VectorsOptions: &qdrant.VectorsOutput_Vectors{Vectors: &qdrant.NamedVectorsOutput{Vectors: named}}}
}

func testVectorOutput(input *qdrant.Vector) *qdrant.VectorOutput {
	if dense := input.GetDense(); dense != nil {
		return &qdrant.VectorOutput{Vector: &qdrant.VectorOutput_Dense{Dense: dense}}
	}
	if sparse := input.GetSparse(); sparse != nil {
		return &qdrant.VectorOutput{Vector: &qdrant.VectorOutput_Sparse{Sparse: sparse}}
	}
	return &qdrant.VectorOutput{}
}
func (m *memoryQdrant) Count(_ context.Context, in *qdrant.CountPoints) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.countErr != nil {
		return 0, m.countErr
	}
	var count uint64
	for _, point := range m.points[in.CollectionName] {
		if matchesFilter(point.Payload, in.Filter) {
			count++
		}
	}
	return count, nil
}

func matchesFilter(payload map[string]*qdrant.Value, filter *qdrant.Filter) bool {
	if filter == nil {
		return true
	}
	for _, condition := range filter.MustNot {
		field := condition.GetField()
		if field != nil && payloadString(payload, field.Key, "") == field.Match.GetKeyword() {
			return false
		}
	}
	for _, condition := range filter.Must {
		field := condition.GetField()
		if field == nil {
			continue
		}
		if field.Match == nil {
			continue
		}
		if integer, ok := field.Match.MatchValue.(*qdrant.Match_Integer); ok {
			if payloadInt(payload, field.Key) != integer.Integer {
				return false
			}
			continue
		}
		if keywords, ok := field.Match.MatchValue.(*qdrant.Match_Keywords); ok {
			if !containsString(keywords.Keywords.Strings, payloadString(payload, field.Key, "")) {
				return false
			}
			continue
		}
		if payloadString(payload, field.Key, "") != field.Match.GetKeyword() {
			return false
		}
	}
	return true
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func specWorker(t *testing.T, q QdrantClient) (*IngestionWorker, string) {
	t.Helper()
	return specWorkerAs(t, q, "writer-1", "change-1", RoleWriter)
}

// specWorkerAs builds a worker for one device sharing the same in-memory
// Qdrant, so tests can exercise several writers and readers of one Hive.
func specWorkerAs(t *testing.T, q QdrantClient, deviceID, approvalID, role string) (*IngestionWorker, string) {
	t.Helper()
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	root = canonicalRoot
	if err := os.MkdirAll(filepath.Join(root, "programs", "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{HiveID: "test-hive", DeviceID: deviceID, WriterApprovalID: approvalID, Role: role, CollectionName: "hive_data", ControlCollection: "hive_data__control", DataDirectory: root, WatchDirectory: root,
		OllamaHost: "http://localhost:11434", EmbeddingModel: "embed-model", MaxEmbeddingWorkers: 1, MaxFileSize: 5 << 20, MaxChunksPerFile: 1000,
		ChunkMaxChars: 2000, ChunkOverlapChars: 200, JSONMaxDepth: 64, JSONMaxElements: 100000, DeleteGrace: 24 * time.Hour, BatchSize: 100, BatchTimeout: time.Hour}
	worker := NewIngestionWorker(cfg, q, nil)
	auditCfg := cfg
	auditCfg.AuditDirectory = filepath.Join(t.TempDir(), "audit")
	worker.Audit, err = OpenFileAudit(auditCfg)
	if err != nil {
		t.Fatal(err)
	}
	fileAudit := worker.Audit.(*FileAudit)
	t.Cleanup(func() { _ = fileAudit.Close() })
	worker.HTTPClient.Transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		body := `{"embedding":[0.1,0.2,0.3]}`
		if req.URL.Path == "/api/tags" {
			body = `{"models":[{"name":"embed-model","digest":"sha256:0123456789abcdef"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	t.Cleanup(worker.Close)
	return worker, root
}

func TestSpec000CreatesControlTopologyAndRegistersEveryWriter(t *testing.T) {
	q := newMemoryQdrant()
	writerA, _ := specWorker(t, q)
	if err := writerA.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !q.collections["hive_data"] || !q.collections["hive_data__control"] {
		t.Fatal("both data and control collections must exist")
	}
	if len(q.points["hive_data__control"]) != 2 {
		t.Fatalf("expected manifest and writer registration, got %d records", len(q.points["hive_data__control"]))
	}
	writerB, _ := specWorkerAs(t, q, "writer-2", "change-2", RoleWriter)
	if err := writerB.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatalf("second authorized writer was rejected: %v", err)
	}
	if len(q.points["hive_data__control"]) != 3 {
		t.Fatalf("expected one registration per writer device, got %d records", len(q.points["hive_data__control"]))
	}
	registrations, err := writerB.writerRegistrations(context.Background())
	if err != nil || len(registrations) != 2 {
		t.Fatalf("expected two writer registrations, got %d (%v)", len(registrations), err)
	}
	for _, w := range []*IngestionWorker{writerA, writerB} {
		w.infrastructureReady = false
		if err := w.ValidateInfrastructure(context.Background()); err != nil {
			t.Fatalf("registered writer %s failed validation: %v", w.Cfg.DeviceID, err)
		}
	}
	writerB.Cfg.WriterApprovalID = "change-9"
	writerB.infrastructureReady = false
	if err := writerB.ValidateInfrastructure(context.Background()); err == nil {
		t.Fatal("writer with a different approval identifier passed validation")
	}
	unregistered, _ := specWorkerAs(t, q, "writer-3", "change-3", RoleWriter)
	if err := unregistered.ValidateInfrastructure(context.Background()); err == nil {
		t.Fatal("unregistered writer device passed validation")
	}
	reader, _ := specWorkerAs(t, q, "reader-1", "", RoleReader)
	if err := reader.ValidateInfrastructure(context.Background()); err != nil {
		t.Fatalf("reader validation failed with registered writers: %v", err)
	}
}

func writeNote(t *testing.T, root, rel, body string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMultiWriterDocumentOwnership(t *testing.T) {
	q := newMemoryQdrant()
	writerA, rootA := specWorker(t, q)
	writerB, rootB := specWorkerAs(t, q, "writer-2", "change-2", RoleWriter)
	pathA := writeNote(t, rootA, "programs/acme/notes/shared.md", "---\nprogram_id: acme\n---\nowned by A")
	if err := writerA.SyncFileState(context.Background(), pathA); err != nil {
		t.Fatal(err)
	}
	docID := deterministicUUID("document", "test-hive", "acme", "programs/acme/notes/shared.md")
	head, _ := writerA.readDocumentHead(context.Background(), docID)
	if head.WriterDeviceID != "writer-1" || head.Bytes != int64(len("---\nprogram_id: acme\n---\nowned by A")) {
		t.Fatalf("head did not record owner and size: %+v", head)
	}
	chunksBefore := len(q.points["hive_data"])

	pathB := writeNote(t, rootB, "programs/acme/notes/shared.md", "---\nprogram_id: acme\n---\nB tries to overwrite")
	if err := writerB.SyncFileState(context.Background(), pathB); !errors.Is(err, errForeignDocument) {
		t.Fatalf("foreign document was not skipped: %v", err)
	}
	if len(q.points["hive_data"]) != chunksBefore {
		t.Fatal("another writer changed the published chunks")
	}
	writeNote(t, rootB, "programs/acme/notes/own.md", "---\nprogram_id: acme\n---\nowned by B")
	summary, err := writerB.SyncWorkspace(context.Background())
	if err != nil || summary.Ingested != 1 || summary.Skipped != 1 {
		t.Fatalf("workspace sync must skip foreign documents without failing: %+v %v", summary, err)
	}
	ownID := deterministicUUID("document", "test-hive", "acme", "programs/acme/notes/own.md")
	if ownHead, _ := writerB.readDocumentHead(context.Background(), ownID); ownHead == nil || ownHead.WriterDeviceID != "writer-2" {
		t.Fatal("writer B did not own its new document")
	}

	err = writerB.RemoveDocument(context.Background(), pathB, "operator_requested")
	var opErr *operationalError
	if !errors.As(err, &opErr) || opErr.code != ExitAuthorization {
		t.Fatalf("removing a foreign document must be an authorization error, got %v", err)
	}
	if head, _ = writerA.readDocumentHead(context.Background(), docID); head.State != "active" {
		t.Fatal("foreign removal changed the head state")
	}

	if err := os.Remove(pathA); err != nil {
		t.Fatal(err)
	}
	if err := writerA.SyncFileState(context.Background(), pathA); err != nil {
		t.Fatal(err)
	}
	if err := writerB.MarkPendingDelete(context.Background(), pathB); !errors.Is(err, errForeignDocument) {
		t.Fatalf("foreign pending delete was not refused: %v", err)
	}
	later := time.Now().Add(25 * time.Hour)
	if removed, err := writerB.PrunePending(context.Background(), later); err != nil || removed != 0 {
		t.Fatalf("writer B pruned a document it does not own: %d %v", removed, err)
	}
	if removed, err := writerA.PrunePending(context.Background(), later); err != nil || removed != 1 {
		t.Fatalf("owner could not prune its pending document: %d %v", removed, err)
	}
}

func TestScopeApprovalOwnedByApprover(t *testing.T) {
	q := newMemoryQdrant()
	writerA, rootA := specWorker(t, q)
	writeScopeFixture(t, rootA, `[{"action":"include","asset_type":"host","value":"api.example.com"}]`)
	if _, err := writerA.SyncWorkspace(context.Background()); err != nil {
		t.Fatal(err)
	}
	preview, err := writerA.ScopeApprovalPreview("acme")
	if err != nil {
		t.Fatal(err)
	}
	if err := writerA.ApproveScopeRevision(context.Background(), "acme", preview.SHA256); err != nil {
		t.Fatal(err)
	}

	writerB, rootB := specWorkerAs(t, q, "writer-2", "change-2", RoleWriter)
	if err := os.MkdirAll(filepath.Join(rootB, "programs", "acme"), 0o755); err != nil {
		t.Fatal(err)
	}
	note := writeNote(t, rootB, "programs/acme/notes/b.md", "---\nprogram_id: acme\nasset_refs: [api.example.com]\n---\nB evidence")
	if err := writerB.SyncFileState(context.Background(), note); err != nil {
		t.Fatalf("writer without a local manifest could not ingest into an approved program: %v", err)
	}
	if revision, _ := writerB.controlScopeRevision(context.Background(), "acme"); revision != preview.SHA256 {
		t.Fatalf("writer B invalidated an approval it does not own: %q", revision)
	}
	noteID := deterministicUUID("document", "test-hive", "acme", "programs/acme/notes/b.md")
	for _, point := range q.points["hive_data"] {
		if payloadString(point.Payload, "document_id", "") == noteID && payloadString(point.Payload, "effective_scope_status", "") != "authorized" {
			t.Fatalf("writer B did not derive scope from the approved manifest: %s", payloadString(point.Payload, "effective_scope_status", ""))
		}
	}
	// The approver's local manifest still governs: changing it revokes scope.
	writeScopeFixture(t, rootA, `[{"action":"include","asset_type":"host","value":"other.example.com"}]`)
	if _, _, err := writerA.resolveActiveScope(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	if revision, _ := writerA.controlScopeRevision(context.Background(), "acme"); revision != "unapproved" {
		t.Fatal("changed manifest on the approving writer did not invalidate the approval")
	}
}

func TestSpec002PublishesCompleteRevisionAndIsIdempotent(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	path := filepath.Join(root, "programs", "acme", "notes.md")
	content := "---\nsource: operator\n---\n# Target\n\n## Hosts\nalpha.example\n\n## Ports\n443"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := worker.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	dataCount := len(q.points["hive_data"])
	if dataCount < 2 {
		t.Fatalf("expected section chunks, got %d", dataCount)
	}
	for _, point := range q.points["hive_data"] {
		chunk := payloadString(point.Payload, "content", "")
		if !strings.Contains(chunk, "# Target") || !strings.Contains(chunk, "source: operator") {
			t.Fatalf("Markdown context was not preserved: %q", chunk)
		}
	}
	headRows, err := worker.controlRows(context.Background(), "document_head", deterministicUUID("document", "test-hive", "acme", "programs/acme/notes.md"))
	if err != nil || len(headRows) != 1 || payloadString(headRows[0].Payload, "state", "") != "active" {
		t.Fatal("active head was not committed")
	}
	if payloadString(headRows[0].Payload, "active_scope_revision", "") != "unapproved" {
		t.Fatal("ordinary content must fail closed to unapproved")
	}
	beforeEvents := len(q.events)
	if err := worker.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if len(q.events) != beforeEvents {
		t.Fatal("unchanged document generated a write")
	}
}

func TestSpec002FailureBeforeCommitKeepsOldHead(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	path := filepath.Join(root, "programs", "acme", "notes.txt")
	if err := os.WriteFile(path, []byte("first revision"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := worker.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	docID := deterministicUUID("document", "test-hive", "acme", "programs/acme/notes.txt")
	oldHead, _ := worker.readDocumentHead(context.Background(), docID)
	if err := os.WriteFile(path, []byte("second revision"), 0o600); err != nil {
		t.Fatal(err)
	}
	worker.HTTPClient.Transport = transportFunc(func(req *http.Request) (*http.Response, error) { return nil, errors.New("embedding unavailable") })
	if err := worker.SyncFileState(context.Background(), path); err == nil {
		t.Fatal("expected embedding failure")
	}
	head, _ := worker.readDocumentHead(context.Background(), docID)
	if head.DocumentRevision != oldHead.DocumentRevision {
		t.Fatal("failed staging changed the active head")
	}
}

func TestSpec002MissingFileOnlyMarksPendingDelete(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	path := filepath.Join(root, "programs", "acme", "notes.txt")
	if err := os.WriteFile(path, []byte("keep me during sync delay"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := worker.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	dataCount := len(q.points["hive_data"])
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := worker.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if len(q.points["hive_data"]) != dataCount {
		t.Fatal("watcher absence deleted chunks")
	}
	docID := deterministicUUID("document", "test-hive", "acme", "programs/acme/notes.txt")
	head, _ := worker.readDocumentHead(context.Background(), docID)
	if head.State != "pending_delete" || head.PendingSince == "" {
		t.Fatal("missing file was not marked pending_delete")
	}
	if err := os.WriteFile(path, []byte("keep me during sync delay"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := worker.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	head, _ = worker.readDocumentHead(context.Background(), docID)
	if head.State != "active" || len(q.points["hive_data"]) != dataCount {
		t.Fatal("reappearing unchanged file was not reactivated without duplicate chunks")
	}
}

func TestSpec002PruneWritesTombstoneBeforeDeleting(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	path := filepath.Join(root, "programs", "acme", "expired.txt")
	if err := os.WriteFile(path, []byte("expired observation"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := worker.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := worker.MarkPendingDelete(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	q.events = nil
	removed, err := worker.PrunePending(context.Background(), time.Now().Add(25*time.Hour))
	if err != nil || removed != 1 {
		t.Fatalf("prune result=%d err=%v", removed, err)
	}
	if len(q.points["hive_data"]) != 0 {
		t.Fatal("prune retained document chunks")
	}
	if len(q.events) < 3 || q.events[0] != "upsert:tombstone" || q.events[1] != "upsert:document_head" || q.events[2] != "delete:chunks" {
		t.Fatalf("unsafe delete order: %v", q.events)
	}
	if err := os.WriteFile(path, []byte("expired observation"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := worker.SyncFileState(context.Background(), path); err == nil || !strings.Contains(err.Error(), "tombstoned") {
		t.Fatal("tombstoned document was silently reingested")
	}
}

func TestSpec002CleanupFailureLeavesNewRevisionActive(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	path := filepath.Join(root, "programs", "acme", "changing.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := worker.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	docID := deterministicUUID("document", "test-hive", "acme", "programs/acme/changing.txt")
	oldHead, _ := worker.readDocumentHead(context.Background(), docID)
	if err := os.WriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	q.deleteErr = errors.New("cleanup unavailable")
	if err := worker.SyncFileState(context.Background(), path); err == nil || !strings.Contains(err.Error(), "cleanup pending") {
		t.Fatal("expected cleanup warning")
	}
	newHead, _ := worker.readDocumentHead(context.Background(), docID)
	if newHead.DocumentRevision == oldHead.DocumentRevision || newHead.State != "active" {
		t.Fatal("cleanup failure rolled back the new head")
	}
	var candidates []*qdrant.ScoredPoint
	for _, point := range q.points["hive_data"] {
		candidates = append(candidates, &qdrant.ScoredPoint{Payload: point.Payload})
	}
	active, err := worker.filterActiveCandidates(context.Background(), candidates)
	if err != nil || len(active) != 1 || payloadString(active[0].Payload, "document_revision", "") != newHead.DocumentRevision {
		t.Fatalf("inactive revision escaped filtering: count=%d err=%v", len(active), err)
	}
	if err := worker.RemoveDocument(context.Background(), path, "operator_requested"); err == nil {
		t.Fatal("expected configured cleanup failure")
	}
	active, err = worker.filterActiveCandidates(context.Background(), candidates)
	if err != nil || len(active) != 0 {
		t.Fatal("tombstoned chunks remained visible")
	}
}

func TestSpec002RejectsExternalSymlinkAndDeepJSON(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	external := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(external, []byte("secret"), 0o600)
	link := filepath.Join(root, "programs", "acme", "link.txt")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	if err := worker.SyncFileState(context.Background(), link); err == nil {
		t.Fatal("external symlink was accepted")
	}
	deep := strings.Repeat(`{"x":`, 65) + `0` + strings.Repeat(`}`, 65)
	jsonPath := filepath.Join(root, "programs", "acme", "deep.json")
	_ = os.WriteFile(jsonPath, []byte(deep), 0o600)
	if err := worker.SyncFileState(context.Background(), jsonPath); err == nil {
		t.Fatal("deep JSON was accepted")
	}
}

func TestSpec002RejectsBinarySizeAndChunkLimits(t *testing.T) {
	t.Run("binary", func(t *testing.T) {
		q := newMemoryQdrant()
		worker, root := specWorker(t, q)
		path := filepath.Join(root, "programs", "acme", "binary.txt")
		if err := os.WriteFile(path, []byte{0, 1, 2, 3}, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := worker.SyncFileState(context.Background(), path); err == nil {
			t.Fatal("binary document was accepted")
		}
	})
	t.Run("size", func(t *testing.T) {
		q := newMemoryQdrant()
		worker, root := specWorker(t, q)
		worker.Cfg.MaxFileSize = 4
		path := filepath.Join(root, "programs", "acme", "large.txt")
		if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := worker.SyncFileState(context.Background(), path); err == nil {
			t.Fatal("oversized document was accepted")
		}
	})
	t.Run("chunks", func(t *testing.T) {
		q := newMemoryQdrant()
		worker, root := specWorker(t, q)
		worker.Cfg.ChunkMaxChars = 4
		worker.Cfg.ChunkOverlapChars = 0
		worker.Cfg.MaxChunksPerFile = 1
		path := filepath.Join(root, "programs", "acme", "chunks.txt")
		if err := os.WriteFile(path, []byte("123456789"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := worker.SyncFileState(context.Background(), path); err == nil {
			t.Fatal("chunk-count limit was not enforced")
		}
	})
}

func TestJSONNormalizationPreservesNumbersAndSortsKeys(t *testing.T) {
	normalized, err := normalizeJSON([]byte(`{"z":9007199254740993,"a":"value"}`), 64, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(normalized, "9007199254740993") || strings.Index(normalized, `"a"`) > strings.Index(normalized, `"z"`) {
		t.Fatalf("unexpected normalized JSON: %s", normalized)
	}
}
