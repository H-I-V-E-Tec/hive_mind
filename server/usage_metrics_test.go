package server

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qdrant/go-client/qdrant"
)

func readAuditEvents(t *testing.T, worker *IngestionWorker) ([]AuditEvent, string) {
	t.Helper()
	audit := worker.Audit.(*FileAudit)
	entries, err := os.ReadDir(audit.cfg.AuditDirectory)
	if err != nil {
		t.Fatal(err)
	}
	var events []AuditEvent
	var raw strings.Builder
	for _, entry := range entries {
		f, err := os.Open(filepath.Join(audit.cfg.AuditDirectory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			raw.WriteString(scanner.Text())
			var event AuditEvent
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				t.Fatalf("invalid audit line: %v", err)
			}
			events = append(events, event)
		}
		f.Close()
	}
	return events, raw.String()
}

func setDocumentBytes(q *memoryQdrant, documentID string, size int64) {
	id := deterministicUUID("control", "test-hive", "document_head", documentID)
	q.points["hive_data__control"][id].Payload["document_bytes"] = qdrant.NewValueInt(size)
}

func TestSpec009RetrievalToolsAuditUsageMetricsWithoutContent(t *testing.T) {
	worker, q, _, revision := setupContextWorker(t, `[{"action":"include","asset_type":"wildcard_domain","value":"*.example.com"}]`)
	documents := map[string]string{"rules-doc": "programs/acme/rules.md", "note-doc": "programs/acme/notes/api.md"}
	addSearchControlFixture(t, q, revision, documents)
	setDocumentBytes(q, "rules-doc", 4000)
	setDocumentBytes(q, "note-doc", 9000)
	q.queryResp = []*qdrant.ScoredPoint{
		contextPoint("note-doc", documents["note-doc"], "note", revision, "authorized", "api.example.com", "SENTINEL-NOTE-TEXT chunk one", 0.95),
		contextPoint("note-doc", documents["note-doc"], "note", revision, "authorized", "api.example.com", "SENTINEL-NOTE-TEXT chunk two", 0.9),
		contextPoint("rules-doc", documents["rules-doc"], "rules", revision, "unknown", "", "SENTINEL-RULES-TEXT", 0.4),
	}
	// Both chunks of note-doc share one content hash in the fixture; keep ordinals distinct.
	q.queryResp[1].Payload["chunk_ordinal"] = qdrant.NewValueInt(1)

	response, err := worker.HiveSearch(context.Background(), HiveSearchArguments{ProgramID: "acme", Query: "SENTINEL-QUERY oauth"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 3 {
		t.Fatalf("expected three results, got %d", len(response.Results))
	}
	events, raw := readAuditEvents(t, worker)
	var search *AuditEvent
	for i := range events {
		if events[i].Action == "hive_search" {
			search = &events[i]
		}
	}
	if search == nil {
		t.Fatal("hive_search was not audited")
	}
	encoded, _ := json.Marshal(response)
	if search.ResponseChars != int64(len(encoded)) || search.ResponseChars == 0 {
		t.Fatalf("response_chars=%d, serialized response is %d", search.ResponseChars, len(encoded))
	}
	if search.SourceBytes != 13000 {
		t.Fatalf("source_bytes must sum distinct documents once: %d", search.SourceBytes)
	}
	if search.Count != 3 || search.Truncated {
		t.Fatalf("unexpected counters: %+v", search)
	}
	for _, sentinel := range []string{"SENTINEL-NOTE-TEXT", "SENTINEL-RULES-TEXT", "SENTINEL-QUERY"} {
		if strings.Contains(raw, sentinel) {
			t.Fatalf("audit log leaked %s", sentinel)
		}
	}

	searchesBefore := 0
	for _, event := range events {
		if event.Action == "hive_search" {
			searchesBefore++
		}
	}
	contextResponse, err := worker.HiveGetContext(context.Background(), HiveContextArguments{
		ProgramID: "acme", Question: "SENTINEL-QUESTION", Asset: ContextAsset{Type: "host", Value: "api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	events, raw = readAuditEvents(t, worker)
	searchesAfter, contexts := 0, 0
	var contextEvent AuditEvent
	for _, event := range events {
		switch event.Action {
		case "hive_search":
			searchesAfter++
		case "hive_get_context":
			contexts++
			contextEvent = event
		}
	}
	if contexts != 1 || searchesAfter != searchesBefore {
		t.Fatalf("nested searches must not be audited separately: contexts=%d searches before=%d after=%d", contexts, searchesBefore, searchesAfter)
	}
	encoded, _ = json.Marshal(contextResponse)
	if contextEvent.ResponseChars != int64(len(encoded)) || contextEvent.SourceBytes != 13000 || contextEvent.Count != len(contextResponse.Items) {
		t.Fatalf("context metrics mismatch: %+v (serialized %d, items %d)", contextEvent, len(encoded), len(contextResponse.Items))
	}
	if strings.Contains(raw, "SENTINEL-QUESTION") {
		t.Fatal("audit log leaked the context question")
	}
}

func TestSpec009AuditReportAggregatesUsageWithoutContent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "audit")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"time":"2026-09-14T10:00:00Z","device_id":"alice-laptop","role":"writer","action":"hive_search","outcome":"completed","program_id":"prog-a","count":5,"duration_ms":120,"response_chars":4000,"source_bytes":40000}`,
		`{"time":"2026-09-14T11:00:00Z","device_id":"alice-laptop","role":"writer","action":"hive_get_context","outcome":"completed","program_id":"prog-a","count":8,"duration_ms":300,"response_chars":9000,"source_bytes":90000,"truncated":true}`,
		`{"time":"2026-09-15T09:00:00Z","device_id":"bob-laptop","role":"writer","action":"hive_search","outcome":"failed","program_id":"prog-b","count":0,"duration_ms":10}`,
		`{"time":"2026-09-13T09:00:00Z","device_id":"bob-laptop","role":"writer","action":"hive_search","outcome":"completed","program_id":"prog-b","count":2,"duration_ms":50,"response_chars":1000,"source_bytes":5000}`,
		`{"time":"2026-09-14T12:00:00Z","device_id":"alice-laptop","role":"writer","action":"document_head","outcome":"committed","program_id":"prog-a"}`,
		`not json SENTINEL-GARBAGE`,
	}
	if err := os.WriteFile(filepath.Join(dir, "audit-one.jsonl"), []byte(strings.Join(lines[:2], "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audit-two.jsonl"), []byte(strings.Join(lines[2:], "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := BuildAuditUsageReport(dir, time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Files != 2 || report.Events != 4 || len(report.Rows) != 4 {
		t.Fatalf("unexpected aggregation: files=%d events=%d rows=%d", report.Files, report.Events, len(report.Rows))
	}
	if report.Rows[0].Day != "2026-09-13" || report.Rows[1].Tool != "hive_get_context" || report.Rows[3].DeviceID != "bob-laptop" || report.Rows[3].Failed != 1 {
		t.Fatalf("rows are not ordered by day, device, program, tool: %+v", report.Rows)
	}
	if report.Totals.Queries != 4 || report.Totals.Failed != 1 || report.Totals.Truncated != 1 || report.Totals.ResponseChars != 14000 || report.Totals.SourceBytes != 135000 {
		t.Fatalf("totals are wrong: %+v", report.Totals)
	}
	if report.Totals.SavingsRatio < 9.6 || report.Totals.SavingsRatio > 9.7 {
		t.Fatalf("ratio must be source/response: %f", report.Totals.SavingsRatio)
	}
	encoded, _ := json.Marshal(report)
	if strings.Contains(string(encoded), "SENTINEL-GARBAGE") || !strings.Contains(string(encoded), "unreadable") {
		t.Fatal("unreadable lines must be counted, never echoed")
	}

	since, program, err := parseAuditReportArgs([]string{"--since=2026-09-14", "--program=prog-a"})
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := BuildAuditUsageReport(dir, since, program)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Events != 2 || filtered.Totals.SourceBytes != 130000 || filtered.Since == "" {
		t.Fatalf("filters were not applied: %+v", filtered)
	}
	for _, bad := range [][]string{{"--since=yesterday"}, {"--program=Bad Program"}, {"--limit=3"}, {"--since"}} {
		if _, _, err := parseAuditReportArgs(bad); err == nil {
			t.Fatalf("invalid arguments accepted: %v", bad)
		}
	}
	if _, err := BuildAuditUsageReport("", time.Time{}, ""); err == nil {
		t.Fatal("report without audit directory must fail")
	}
}

func TestSpec009AuditReportCommandLineIsAccepted(t *testing.T) {
	if _, err := splitCLIArgs([]string{"hive", "audit", "report", "--since=2026-09-14", "--program=prog-a"}); err != nil {
		t.Fatalf("valid audit report invocation rejected: %v", err)
	}
	if _, _, err := parseConfigFlags([]string{"audit", "report", "--since=2026-09-14", "--program=prog-a"}); err != nil {
		t.Fatalf("report flags must not be treated as configuration: %v", err)
	}
	for _, bad := range [][]string{
		{"hive", "audit", "report", "--since=yesterday"},
		{"hive", "audit", "report", "--limit=3"},
		{"hive", "audit", "bogus"},
		{"hive", "audit"},
	} {
		if _, err := splitCLIArgs(bad); err == nil {
			t.Fatalf("invalid invocation accepted: %v", bad)
		}
	}
}
