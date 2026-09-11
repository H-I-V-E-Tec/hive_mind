package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type failingAudit struct{}

func (failingAudit) Record(AuditEvent) error { return errors.New("synthetic credential must not leak") }

func TestSecurity007AuditFailureBlocksCriticalWrites(t *testing.T) {
	q := newMemoryQdrant()
	w, _ := specWorker(t, q)
	if err := w.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := len(q.events)
	w.Audit = failingAudit{}
	err := w.upsertControl(context.Background(), "tombstone", "doc", map[string]any{"document_id": "doc"})
	if err == nil || classifyOperationalError(err) != ExitPartialFailure {
		t.Fatalf("audit failure not enforced: %v", err)
	}
	if len(q.events) != before {
		t.Fatal("mutation occurred after failed audit")
	}
	report, err := w.OperationalStatus(context.Background())
	if err != nil || len(report.Warnings) == 0 {
		t.Fatalf("audit failure invisible: %+v %v", report, err)
	}
}

func TestSecurity007AuditRedactionRotationAndRetention(t *testing.T) {
	cfg := Config{AuditDirectory: filepath.Join(t.TempDir(), "audit"), HiveID: "test", DeviceID: "writer", Role: RoleWriter, QdrantAPIKey: newSecret("sentinel-secret")}
	a, err := OpenFileAudit(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.maxBytes = 400
	expired := "audit-expired.jsonl"
	if err := os.WriteFile(filepath.Join(cfg.AuditDirectory, expired), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(filepath.Join(cfg.AuditDirectory, expired), past, past); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := a.Record(AuditEvent{Action: "hive_search", Outcome: "completed", Program: "bad\nprogram", Revision: "sentinel-secret", Path: "/private/sentinel-secret"}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(cfg.AuditDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Fatal("audit did not rotate")
	}
	for _, entry := range entries {
		if entry.Name() == expired {
			t.Fatal("expired audit retained")
		}
		info, _ := entry.Info()
		if info.Mode().Perm() != 0600 {
			t.Fatal("audit permissions too broad")
		}
		body, err := os.ReadFile(filepath.Join(cfg.AuditDirectory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "sentinel") || strings.Contains(string(body), "bad") {
			t.Fatal("untrusted detail leaked")
		}
		for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
			var e AuditEvent
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				t.Fatal(err)
			}
			if e.Session == "" || e.Hive != "test" || !strings.HasSuffix(e.Time, "Z") {
				t.Fatal("missing audit provenance")
			}
		}
	}
}

func TestSecurity007RejectsAuditInsideDataAndUnsafePermissions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "audit")
	if _, err := OpenFileAudit(Config{AuditDirectory: dir, DataDirectory: root}); err == nil {
		t.Fatal("indexable audit accepted")
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFileAudit(Config{AuditDirectory: dir}); err == nil {
		t.Fatal("shared audit directory accepted")
	}
}

func TestSpec007ConfirmedScopeHashCannotApproveDifferentFile(t *testing.T) {
	w, root := specWorker(t, newMemoryQdrant())
	path := filepath.Join(root, "programs", "acme", "scope.json")
	body := `{"schema_version":1,"program_id":"acme","source":"operator","collected_at":"2026-09-11T12:00:00Z","rules":[{"action":"include","asset_type":"host","value":"example.com"}]}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	err := w.ApproveScopeRevision(context.Background(), "acme", strings.Repeat("0", 64))
	if err == nil || classifyOperationalError(err) != ExitUsage {
		t.Fatalf("changed hash accepted: %v", err)
	}
}
