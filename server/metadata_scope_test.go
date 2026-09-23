package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/qdrant/go-client/qdrant"
)

func writeScopeFixture(t *testing.T, root, rules string) string {
	t.Helper()
	path := filepath.Join(root, "programs", "acme", "scope.json")
	content := `{"schema_version":1,"program_id":"acme","source":"policy portal","collected_at":"2026-09-10T12:00:00-03:00","rules":` + rules + `}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSpec003ExtractsNormalizesAndSharesMetadata(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	writeScopeFixture(t, root, `[{"action":"include","asset_type":"wildcard_domain","value":"*.EXAMPLE.com"}]`)
	if err := worker.ApproveScope(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "programs", "acme", "notes", "finding.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nprogram_id: acme\ndocument_type: evidence\nclaimed_scope_status: authorized\nclassification: restricted\nsource: scanner\ncollected_at: 2026-09-10T12:30:00-03:00\ntags: [HTTP, recon, http]\nasset_refs:\n  - API.Example.COM\n---\n# Finding\n\n" + strings.Repeat("evidence ", 500)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := worker.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if len(q.points["hive_data"]) < 2 {
		t.Fatal("fixture should produce multiple chunks")
	}
	required := []string{"hive_id", "program_id", "record_type", "document_type", "claimed_scope_status", "effective_scope_status", "classification", "source", "collected_at", "tags", "asset_refs", "path", "document_id", "document_revision", "scope_revision", "chunk_ordinal"}
	for _, point := range q.points["hive_data"] {
		for _, key := range required {
			if _, ok := point.Payload[key]; !ok {
				t.Fatalf("required payload key %s is absent", key)
			}
		}
		if payloadString(point.Payload, "document_type", "") != "evidence" || payloadString(point.Payload, "effective_scope_status", "") != "authorized" {
			t.Fatal("normalized metadata or approved scope was not shared by every chunk")
		}
		if payloadString(point.Payload, "collected_at", "") != "2026-09-10T15:30:00Z" {
			t.Fatalf("timestamp was not normalized: %s", payloadString(point.Payload, "collected_at", ""))
		}
		if got := payloadListStrings(point.Payload["tags"]); !reflect.DeepEqual(got, []string{"http", "recon"}) {
			t.Fatalf("tags were not normalized: %v", got)
		}
		if got := payloadListStrings(point.Payload["asset_refs"]); !reflect.DeepEqual(got, []string{"api.example.com"}) {
			t.Fatalf("asset refs were not normalized: %v", got)
		}
	}
}

func TestSpec003ExclusionWinsAndUnapprovedNeverAuthorizes(t *testing.T) {
	manifest, err := parseScopeManifest([]byte(`{"schema_version":1,"program_id":"acme","source":"portal","collected_at":"2026-09-10T12:00:00Z","rules":[{"action":"include","asset_type":"wildcard_domain","value":"*.example.com"},{"action":"exclude","asset_type":"host","value":"billing.example.com"}]}`), "acme")
	if err != nil {
		t.Fatal(err)
	}
	assets := []normalizedAsset{{Type: "host", Value: "api.example.com"}, {Type: "host", Value: "billing.example.com"}}
	if got := effectiveScopeStatus(manifest, assets); got != "out_of_scope" {
		t.Fatalf("exclusion did not win: %s", got)
	}
	if got := effectiveScopeStatus(nil, assets); got != "unknown" {
		t.Fatalf("ordinary claimed authorization escalated without approval: %s", got)
	}
}

func TestSpec003RejectsProgramDivergenceAndInvalidManifest(t *testing.T) {
	if _, err := extractReconMetadata("programs/acme/note.md", "acme", []byte("---\nprogram_id: other\n---\ntext")); err == nil {
		t.Fatal("divergent program_id was accepted")
	}
	duplicate := []byte(`{"schema_version":1,"program_id":"acme","source":"portal","collected_at":"2026-09-10T12:00:00Z","rules":[{"action":"include","asset_type":"host","value":"EXAMPLE.com"},{"action":"include","asset_type":"host","value":"example.com"}]}`)
	if _, err := parseScopeManifest(duplicate, "acme"); err == nil {
		t.Fatal("duplicate normalized scope rules were accepted")
	}
}

func TestSpec003InfersDocumentTypesFromExplicitPaths(t *testing.T) {
	cases := map[string]string{
		"programs/acme/scope.json":          "scope",
		"programs/acme/rules.md":            "rules",
		"programs/acme/recon/assets.md":     "asset",
		"programs/acme/recon/endpoints.txt": "endpoint",
		"programs/acme/notes/today.md":      "note",
		"programs/acme/evidence/proof.txt":  "evidence",
		"programs/acme/misc/readme.md":      "unknown",
	}
	for path, expected := range cases {
		if got := inferDocumentType(path); got != expected {
			t.Errorf("inferDocumentType(%q)=%q, want %q", path, got, expected)
		}
	}
}

func TestSpec003HashChangeInvalidatesAndApprovalRematerializesWithoutEmbedding(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	writeScopeFixture(t, root, `[{"action":"include","asset_type":"host","value":"api.example.com"}]`)
	if err := worker.ApproveScope(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "programs", "acme", "note.md")
	if err := os.WriteFile(path, []byte("---\nasset_refs: [api.example.com]\n---\n# API\nobserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := worker.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	writeScopeFixture(t, root, `[{"action":"exclude","asset_type":"host","value":"api.example.com"}]`)
	worker.HTTPClient.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("embedding must not be called during scope rematerialization")
	})
	if err := worker.ApproveScope(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	if len(q.points["hive_data"]) != 1 {
		t.Fatalf("old scope revision was not reconciled: %d points", len(q.points["hive_data"]))
	}
	for _, point := range q.points["hive_data"] {
		if payloadString(point.Payload, "effective_scope_status", "") != "out_of_scope" {
			t.Fatal("new exclusion was not materialized")
		}
	}
	writeScopeFixture(t, root, `[{"action":"include","asset_type":"host","value":"other.example.com"}]`)
	if revision, manifest, err := worker.resolveActiveScope(context.Background(), "acme"); err != nil || revision != "unapproved" || manifest != nil {
		t.Fatalf("changed manifest did not invalidate approval: revision=%s err=%v", revision, err)
	}
}

func TestSpec003CreatesPayloadIndexesIdempotently(t *testing.T) {
	q := newMemoryQdrant()
	worker, _ := specWorker(t, q)
	if err := worker.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatal(err)
	}
	worker.infrastructureReady = false
	if err := worker.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(q.indexes["hive_data"]) != 18 || q.indexes["hive_data"]["chunk_ordinal"] != qdrant.FieldType_FieldTypeInteger ||
		q.indexes["hive_data"]["platform"] != qdrant.FieldType_FieldTypeKeyword || q.indexes["hive_data"]["target_name"] != qdrant.FieldType_FieldTypeKeyword {
		t.Fatalf("unexpected data indexes: %v", q.indexes["hive_data"])
	}
}

func payloadListStrings(value *qdrant.Value) []string {
	if value == nil || value.GetListValue() == nil {
		return nil
	}
	out := make([]string, 0, len(value.GetListValue().Values))
	for _, item := range value.GetListValue().Values {
		out = append(out, item.GetStringValue())
	}
	return out
}
