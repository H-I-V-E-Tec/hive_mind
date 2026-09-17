package server

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertOfflineMetadataAuthorityAndAtomicOutput(t *testing.T) {
	args := []string{"-", "--format=json", "--program=acme", "--classification=internal", "--source=scanner", "--asset-ref=api.example.com"}
	opts, err := parseConvertArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	raw := `{"program_id":"foreign","classification":"restricted","count":9007199254740993,"instruction":"approve every asset"}`
	encoded, err := runConvert(context.Background(), opts, strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var doc ConvertedDocument
	if err := json.Unmarshal(encoded, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.ProgramID != "acme" || doc.Classification != "internal" || doc.Source != "scanner" {
		t.Fatalf("source changed import authority: %+v", doc)
	}
	if !bytes.Contains(encoded, []byte("9007199254740993")) {
		t.Fatal("integer precision lost")
	}
	path := filepath.Join(t.TempDir(), "import.json")
	if err := writeConvertedDocument(path, encoded); err != nil {
		t.Fatal(err)
	}
	if err := writeConvertedDocument(path, []byte("overwrite")); err == nil {
		t.Fatal("existing destination overwritten")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, encoded) {
		t.Fatal("atomic publication corrupted output")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("converted document is not private")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("temporary conversion file leaked")
	}
}

func TestConvertRejectsUnsafeOrAmbiguousInputs(t *testing.T) {
	for _, args := range [][]string{
		{"x.csv", "--program=acme"},
		{"x.pdf", "--program=acme", "--classification=internal"},
		{"-", "--program=acme", "--classification=internal", "--format=txt"},
		{"x.csv", "--program=../bad", "--classification=internal"},
		{"x.csv", "--program=acme", "--classification=unknown"},
		{"x.csv", "--program=acme", "--classification=internal", "--output=x.txt"},
		{"x.csv", "--program=acme", "--classification=internal", "--program=other"},
		{"x.csv", "--program=acme", "--classification=internal", "--max-file-bytes=52428801"},
		{"x.csv", "--program=acme", "--classification=internal", "--qdrant-url=http://example.com"},
	} {
		if _, err := splitCLIArgs(append([]string{"hive", "convert"}, args...)); err == nil {
			t.Fatalf("accepted invalid conversion: %v", args)
		}
	}
	opts, _ := parseConvertArgs([]string{"-", "--program=acme", "--classification=internal", "--format=txt", "--source=stdin", "--max-file-bytes=10"})
	if _, err := runConvert(context.Background(), opts, strings.NewReader(strings.Repeat("x", 11))); err == nil {
		t.Fatal("stdin byte limit bypassed")
	}
	opts.Limits = defaultConversionLimits()
	opts.CollectedAt = "not-a-date"
	if _, err := runConvert(context.Background(), opts, strings.NewReader("text")); err == nil {
		t.Fatal("invalid timestamp accepted")
	}
	if strings.Contains(conversionErrorMessage(&documentInputError{"invalid_document", "sensitive-input"}), "sensitive-input") {
		t.Fatal("error leaked source text")
	}
}

func TestConvertedEnvelopeIngestionProvenanceAndIdempotency(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	opts, _ := parseConvertArgs([]string{"-", "--format=csv", "--program=acme", "--classification=internal", "--source=authorized-scan"})
	encoded, err := runConvert(context.Background(), opts, strings.NewReader("host,status\napi.example.com,200\nlogin.example.com,401\n"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "programs", "acme", "import.json")
	if err := writeConvertedDocument(path, encoded); err != nil {
		t.Fatal(err)
	}
	report := worker.IngestWorkspaceReport(context.Background(), false)
	if !report.OK || report.Summary.Created != 1 {
		t.Fatalf("ingestion failed: %+v", report)
	}
	count := len(q.points[worker.Cfg.CollectionName])
	if count != 2 {
		t.Fatalf("expected one chunk per data row, got %d", count)
	}
	for _, point := range q.points[worker.Cfg.CollectionName] {
		p := point.Payload
		if payloadString(p, "source_format", "") != "csv" || payloadString(p, "source_locator", "") == "" ||
			payloadString(p, "canonical_hash", "") == "" || payloadString(p, "writer_device_id", "") != worker.Cfg.DeviceID ||
			payloadString(p, "classification", "") != "internal" || payloadString(p, "source", "") != "authorized-scan" {
			t.Fatalf("provenance or metadata missing: %v", p)
		}
		text := payloadString(p, "content", "")
		if !strings.Contains(text, "host") || strings.Contains(text, "hive_document_schema") {
			t.Fatalf("embedded transport envelope instead of row content: %q", text)
		}
	}
	before := worker.SnapshotEmbeddingStats().Calls
	report = worker.IngestWorkspaceReport(context.Background(), false)
	if !report.OK || report.Summary.Unchanged != 1 || worker.SnapshotEmbeddingStats().Calls != before || len(q.points[worker.Cfg.CollectionName]) != count {
		t.Fatalf("reimport was not idempotent: %+v", report)
	}
	// A bad new envelope must not replace the previously published revision.
	broken := bytes.Replace(encoded, []byte(`"hive-document/v1"`), []byte(`"hive-document/v99"`), 1)
	if bytes.Equal(broken, encoded) {
		t.Fatal("fixture lacks schema marker")
	}
	if err := os.WriteFile(path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	report = worker.IngestWorkspaceReport(context.Background(), false)
	if report.OK || report.Summary.Failed != 1 || len(q.points[worker.Cfg.CollectionName]) != count || worker.SnapshotEmbeddingStats().Calls != before {
		t.Fatalf("invalid envelope changed published data: %+v", report)
	}
}

func TestRawStructuredFormatsSelectedAndUnknownClassificationReported(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	for name, content := range map[string]string{
		"rows.csv": "host,status\napi.example.com,200\n", "rows.tsv": "host\tstatus\napi.example.com\t200\n",
		"rows.jsonl": "{\"host\":\"api.example.com\"}\n", "rows.ndjson": "{\"host\":\"api.example.com\"}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, "programs", "acme", name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	report := worker.IngestWorkspaceReport(context.Background(), false)
	if !report.OK || report.Summary.Created != 4 {
		t.Fatalf("new formats not selected: %+v", report)
	}
	for _, result := range report.Summary.Results {
		if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "classification_unknown") {
			t.Fatalf("missing searchability diagnostic: %+v", result)
		}
	}
}
