package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qdrant/go-client/qdrant"
)

func TestScopeStatusRemoteReadOnlyContract(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	setTestHome(t, t.TempDir())
	httpServer := httptest.NewServer(srv.Handler())
	defer httpServer.Close()
	client := NewRemoteClient(httpServer.URL)
	program := "scope-status-lab"
	admin := memberToken(t, srv, key, kid, []string{permissionMindRead, permissionScopeAdmin}, nil)
	reader := memberToken(t, srv, key, kid, []string{permissionMindRead}, nil)
	if err := storeToken(admin); err != nil {
		t.Fatal(err)
	}
	before, err := client.ScopeStatus(context.Background(), program)
	if err != nil || before.Status != "unapproved" || before.ScopeRevision != "unapproved" || len(before.Rules) != 0 {
		t.Fatalf("unapproved scope status: %+v %v", before, err)
	}
	manifest := `{"schema_version":1,"program_id":"scope-status-lab","classification":"internal","source":"synthetic portal","collected_at":"2026-10-23T12:00:00Z","rules":[{"action":"include","asset_type":"host","value":"API.Example.test","reason":"synthetic note"},{"action":"exclude","asset_type":"host","value":"billing.example.test"}]}`
	args := HiveIngestDocumentArguments{ProgramID: program, Classification: "internal", DocumentType: "scope", SourceFormat: "json", Content: manifest}
	if report := client.IngestDocument(context.Background(), args); !report.OK {
		t.Fatalf("synthetic scope publication failed: %+v", report)
	}
	if _, err := client.ApproveScopeRevision(context.Background(), program, sha256Hex([]byte(manifest))); err != nil {
		t.Fatal(err)
	}
	q := srv.worker.QdrantClient.(*memoryQdrant)
	q.mu.Lock()
	eventsBefore := len(q.events)
	q.mu.Unlock()
	if _, err := srv.worker.ScopeStatus(context.Background(), program); err != nil {
		t.Fatalf("synthetic approved scope could not be reconstructed: %v", err)
	}
	approved, err := client.ScopeStatus(context.Background(), program)
	if err != nil || approved.Status != "approved" || approved.SchemaVersion != 1 ||
		approved.ProgramID != program || approved.ScopeRevision != sha256Hex([]byte(manifest)) ||
		len(approved.Rules) != 2 || approved.Rules[0].Value != "api.example.test" {
		t.Fatalf("approved scope status: %+v %v", approved, err)
	}
	encoded, err := json.Marshal(approved)
	if err != nil || strings.Contains(string(encoded), "synthetic note") || strings.Contains(string(encoded), "classification") ||
		len(encoded) > maxScopeStatusBytes {
		t.Fatal("scope status leaked a non-contract field")
	}
	q.mu.Lock()
	eventsAfter := len(q.events)
	q.mu.Unlock()
	if eventsAfter != eventsBefore {
		t.Fatal("scope status wrote to the collection")
	}
	approvalID := deterministicUUID("control", srv.worker.Cfg.HiveID, "scope_approval", program)
	q.mu.Lock()
	approval := q.points[srv.worker.Cfg.ControlCollection][approvalID]
	originalHash := approval.Payload["scope_document_hash"]
	approval.Payload["scope_document_hash"] = qdrant.NewValueString(strings.Repeat("b", 64))
	q.mu.Unlock()
	if _, err := client.ScopeStatus(context.Background(), program); err == nil {
		t.Fatal("approval with a mismatched normalized document hash returned rules")
	}
	q.mu.Lock()
	approval.Payload["scope_document_hash"] = originalHash
	q.mu.Unlock()
	if err := storeToken(reader); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ScopeStatus(context.Background(), program); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("ordinary reader received approved rules: %v", err)
	}
	if err := storeToken(admin); err != nil {
		t.Fatal(err)
	}
	q.mu.Lock()
	for id, point := range q.points[srv.worker.Cfg.CollectionName] {
		if payloadString(point.Payload, "program_id", "") == program && payloadString(point.Payload, "document_type", "") == "scope" {
			delete(q.points[srv.worker.Cfg.CollectionName], id)
		}
	}
	q.mu.Unlock()
	if _, err := client.ScopeStatus(context.Background(), program); err == nil {
		t.Fatal("incomplete approved scope document was returned")
	}
}

func TestScopeStatusSnapshotRejectsInvalidContract(t *testing.T) {
	valid := ScopeStatusSnapshot{SchemaVersion: 1, ProgramID: "lab", Status: "approved",
		ScopeRevision: strings.Repeat("a", 64), Rules: []ScopeStatusRule{{Action: "include", AssetType: "host", Value: "api.example.test"}}}
	if !validScopeStatusSnapshot(valid, "lab") {
		t.Fatal("valid synthetic scope status was rejected")
	}
	for _, mutate := range []func(*ScopeStatusSnapshot){
		func(s *ScopeStatusSnapshot) { s.ProgramID = "other" },
		func(s *ScopeStatusSnapshot) { s.ScopeRevision = "unapproved" },
		func(s *ScopeStatusSnapshot) { s.Rules[0].Value = "API.Example.test" },
		func(s *ScopeStatusSnapshot) { s.Rules[0].Action = "unknown" },
		func(s *ScopeStatusSnapshot) { s.Rules = append(s.Rules, s.Rules[0]) },
		func(s *ScopeStatusSnapshot) { s.Rules[0].Action = "exclude" },
	} {
		candidate := valid
		candidate.Rules = append([]ScopeStatusRule(nil), valid.Rules...)
		mutate(&candidate)
		if validScopeStatusSnapshot(candidate, "lab") {
			t.Fatalf("invalid scope status passed: %+v", candidate)
		}
	}
}
