package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIngestionReportAccountsForEveryFileAndRetainsAllFailures(t *testing.T) {
	q := newMemoryQdrant()
	owner, ownerRoot := specWorker(t, q)
	path := writeNote(t, ownerRoot, "programs/acme/foreign.txt", "owned by the first writer")
	if err := owner.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	worker, root := specWorkerAs(t, q, "writer-2", "change-2", RoleWriter)
	worker.Cfg.MaxEmbeddingWorkers = 4
	for _, name := range []string{"unchanged.txt", "updated.txt"} {
		path := writeNote(t, root, "programs/acme/"+name, "previous observation")
		if err := worker.SyncFileState(context.Background(), path); err != nil {
			t.Fatal(err)
		}
	}
	writeNote(t, root, "programs/acme/updated.txt", "new observation")
	writeNote(t, root, "programs/acme/created.txt", "first observation")
	writeNote(t, root, "programs/acme/foreign.txt", "a local copy owned by another writer")
	writeNote(t, root, "programs/acme/bad.json", `{"secret-sentinel":`)
	writeNote(t, root, "programs/acme/empty.txt", "")
	writeNote(t, root, "programs/acme/binary.txt", "\x00secret-sentinel")
	writeNote(t, root, "programs/acme/unsupported.pdf", "not a supported input")

	report := worker.IngestWorkspaceReport(context.Background(), true)
	s := report.Summary
	if report.OK || report.ExitCode != ExitPartialFailure || report.PruneRun {
		t.Fatalf("partial ingestion must fail without pruning: %+v", report)
	}
	if !s.ScanComplete || s.Total != 7 || s.Ingested != 2 || s.Created != 1 || s.Updated != 1 || s.Unchanged != 1 || s.Skipped != 1 || s.Failed != 3 || s.Cancelled != 0 {
		t.Fatalf("incorrect accounting: %+v", s)
	}
	if len(s.Results) != s.Total {
		t.Fatal("report lost file outcomes")
	}
	want := map[string]SyncOutcome{
		"bad.json": SyncFailed, "empty.txt": SyncFailed, "binary.txt": SyncFailed,
		"created.txt": SyncCreated, "updated.txt": SyncUpdated,
		"unchanged.txt": SyncUnchanged, "foreign.txt": SyncSkipped,
	}
	for i, result := range s.Results {
		if result.Outcome != want[filepath.Base(result.Path)] {
			t.Errorf("unexpected file outcome: %+v", result)
		}
		if i > 0 && s.Results[i-1].Path >= result.Path {
			t.Fatal("concurrent results must retain deterministic path order")
		}
		if result.Outcome == SyncFailed && (result.ReasonCode == "" || result.Detail == "") {
			t.Fatal("failed file has no safe diagnostic")
		}
		if result.Published != (result.Outcome == SyncCreated || result.Outcome == SyncUpdated) {
			t.Fatalf("incorrect publication flag: %+v", result)
		}
	}
	encoded, _ := json.Marshal(report)
	for _, secret := range []string{root, ownerRoot, "secret-sentinel", "previous observation"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("report leaked private content: %s", encoded)
		}
	}
}

func TestIngestionReportUnchangedDoesNotWriteOrEmbed(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	writeNote(t, root, "programs/acme/note.md", "# Observation\nA synthetic fact.")
	first := worker.IngestWorkspaceReport(context.Background(), false)
	if !first.OK || first.Summary.Created != 1 {
		t.Fatalf("initial ingestion failed: %+v", first)
	}
	var calls atomic.Int32
	worker.HTTPClient.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected embedding")
	})
	before := len(q.events)
	second := worker.IngestWorkspaceReport(context.Background(), false)
	if !second.OK || second.Summary.Ingested != 0 || second.Summary.Unchanged != 1 || second.ExitCode != ExitOK {
		t.Fatalf("unchanged file counted as ingestion: %+v", second)
	}
	if calls.Load() != 0 || len(q.events) != before || second.Summary.Results[0].Published {
		t.Fatal("unchanged revision caused work or claimed publication")
	}
}

func TestIngestionReportExplainsDocumentLimits(t *testing.T) {
	tests := []struct {
		name, content, reason string
		configure             func(*IngestionWorker)
	}{
		{"size", "too large", "file_size_limit", func(w *IngestionWorker) { w.Cfg.MaxFileSize = 3 }},
		{"chunks", "multiple chunks", "chunk_limit", func(w *IngestionWorker) {
			w.Cfg.ChunkMaxChars, w.Cfg.ChunkOverlapChars, w.Cfg.MaxChunksPerFile = 3, 0, 1
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			worker, root := specWorker(t, newMemoryQdrant())
			tc.configure(worker)
			writeNote(t, root, "programs/acme/oversized.txt", tc.content)
			report := worker.IngestWorkspaceReport(context.Background(), false)
			if report.OK || report.ExitCode != ExitPartialFailure || report.Summary.Failed != 1 || report.Summary.Results[0].ReasonCode != tc.reason {
				t.Fatalf("document limit misreported: %+v", report)
			}
			if strings.Contains(report.Error, "service") || strings.Contains(report.Error, "schema") {
				t.Fatal("input limit was misclassified as an infrastructure failure")
			}
		})
	}
}

func TestIngestionReportPreservesPublishedRevisionOnCleanupFailure(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	path := writeNote(t, root, "programs/acme/note.txt", "old observation")
	if err := worker.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	writeNote(t, root, "programs/acme/note.txt", "new observation")
	q.deleteErr = errors.New("private-provider-sentinel")
	report := worker.IngestWorkspaceReport(context.Background(), false)
	if report.OK || report.Summary.Failed != 1 || report.Summary.Ingested != 0 {
		t.Fatalf("cleanup failure reported success: %+v", report)
	}
	result := report.Summary.Results[0]
	if !result.Published || result.ReasonCode != "cleanup_pending" || result.Outcome != SyncFailed {
		t.Fatalf("report lost the confirmed publication: %+v", result)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "private-provider-sentinel") {
		t.Fatal("raw provider error escaped into report")
	}
}

func TestIngestionReportEmbeddingFailureRetainsOldRevision(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	path := writeNote(t, root, "programs/acme/note.txt", "old observation")
	if err := worker.SyncFileState(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	id := deterministicUUID("document", worker.Cfg.HiveID, "acme", "programs/acme/note.txt")
	old, _ := worker.readDocumentHead(context.Background(), id)
	writeNote(t, root, "programs/acme/note.txt", "new observation")
	worker.HTTPClient.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader("private-provider-sentinel")), Header: make(http.Header)}, nil
	})
	report := worker.IngestWorkspaceReport(context.Background(), false)
	result := report.Summary.Results[0]
	if report.OK || result.Published || result.ReasonCode != "embedding_failed" {
		t.Fatalf("embedding failure was not represented: %+v", report)
	}
	head, _ := worker.readDocumentHead(context.Background(), id)
	if head.DocumentRevision != old.DocumentRevision {
		t.Fatal("reporting changed publication behavior")
	}
}

func TestIngestionReportCancellationIncludesUndispatchedFiles(t *testing.T) {
	worker, root := specWorker(t, newMemoryQdrant())
	worker.Cfg.MaxEmbeddingWorkers = 1
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeNote(t, root, "programs/acme/"+name, "synthetic observation")
	}
	if err := worker.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.HTTPClient.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return nil, context.Canceled
	})
	report := worker.IngestWorkspaceReport(ctx, true)
	if report.OK || report.ExitCode != ExitPartialFailure || report.PruneRun || report.Summary.Cancelled != 3 || len(report.Summary.Results) != 3 {
		t.Fatalf("cancellation lost pending files: %+v", report)
	}
	for _, result := range report.Summary.Results {
		if result.Outcome != SyncCancelled || result.Published {
			t.Fatalf("cancelled file reported success: %+v", result)
		}
	}
}

func TestIngestionReportPreflightAndPrune(t *testing.T) {
	t.Run("reader", func(t *testing.T) {
		worker := &IngestionWorker{Cfg: Config{Role: RoleReader}}
		report := worker.IngestWorkspaceReport(context.Background(), false)
		if report.OK || report.ExitCode != ExitAuthorization || report.Summary.ScanComplete || report.Summary.Results == nil {
			t.Fatalf("reader report is incorrect: %+v", report)
		}
	})
	t.Run("cancelled before scan", func(t *testing.T) {
		worker := &IngestionWorker{Cfg: Config{Role: RoleWriter}}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		report := worker.IngestWorkspaceReport(ctx, true)
		if report.OK || report.ExitCode != ExitPartialFailure || report.Summary.ScanComplete || report.PruneRun {
			t.Fatalf("cancelled scan reported completion: %+v", report)
		}
	})
	t.Run("empty scan and prune", func(t *testing.T) {
		worker, _ := specWorker(t, newMemoryQdrant())
		report := worker.IngestWorkspaceReport(context.Background(), true)
		if !report.OK || !report.Summary.ScanComplete || report.Summary.Total != 0 || report.Summary.Results == nil || !report.PruneRun {
			t.Fatalf("empty scan should succeed: %+v", report)
		}
	})
	t.Run("prune failure retains sync summary", func(t *testing.T) {
		q := newMemoryQdrant()
		worker, root := specWorker(t, q)
		path := writeNote(t, root, "programs/acme/removed.txt", "old observation")
		if err := worker.SyncFileState(context.Background(), path); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := worker.MarkPendingDelete(context.Background(), path); err != nil {
			t.Fatal(err)
		}
		worker.Cfg.DeleteGrace = -time.Second
		q.deleteErr = errors.New("private-provider-sentinel")
		writeNote(t, root, "programs/acme/new.txt", "new observation")
		report := worker.IngestWorkspaceReport(context.Background(), true)
		if report.OK || !report.PruneRun || report.ExitCode != ExitPartialFailure || report.Summary.Created != 1 {
			t.Fatalf("prune failure lost sync outcome: %+v", report)
		}
	})
}

func TestIngestionMCPResultIncludesReportOnFailure(t *testing.T) {
	worker, root := specWorker(t, newMemoryQdrant())
	writeNote(t, root, "programs/acme/valid.txt", "observation")
	writeNote(t, root, "programs/acme/empty.txt", "")
	report := worker.IngestWorkspaceReport(context.Background(), false)
	encoded, err := json.Marshal(ingestionMCPResponse(json.RawMessage(`42`), report))
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		ID     int `json:"id"`
		Result struct {
			IsError bool                          `json:"isError"`
			Content []struct{ Type, Text string } `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.ID != 42 || !wire.Result.IsError || len(wire.Result.Content) != 1 || wire.Result.Content[0].Type != "text" {
		t.Fatalf("invalid MCP tool failure: %s", encoded)
	}
	var received IngestionReport
	if err := json.Unmarshal([]byte(wire.Result.Content[0].Text), &received); err != nil {
		t.Fatal(err)
	}
	if received.Summary.Created != 1 || received.Summary.Failed != 1 || len(received.Summary.Results) != 2 {
		t.Fatal("MCP lost the partial ingestion report")
	}
}
