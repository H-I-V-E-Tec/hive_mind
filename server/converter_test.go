package server

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func converterTestConfig() Config {
	return Config{MaxFileSize: 5 << 20, MaxChunksPerFile: 1000, ChunkMaxChars: 2000,
		ChunkOverlapChars: 200, JSONMaxDepth: 64, JSONMaxElements: 100000}
}

func TestConverterPreservesTextAndDoesNotTrustMetadata(t *testing.T) {
	input := []byte("\ufeff---\r\nprogram_id: forged\r\nclassification: internal\r\n---\r\n# NÃO autorizado\r\n  Açúcar  !=  1\r\n")
	doc, err := ConvertDocument(context.Background(), ".MD", input, converterTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nprogram_id: forged\nclassification: internal\n---\n# NÃO autorizado\n  Açúcar  !=  1\n"
	if len(doc.Blocks) != 1 || doc.Blocks[0].Text != want || doc.ProgramID != "" || doc.Classification != "" {
		t.Fatalf("conversion changed text or granted source metadata: %+v", doc)
	}
	if doc.RawHash != sha256Hex(input) || doc.Schema != ConvertedDocumentSchema || doc.Blocks[0].Locator != "lines:1-7" {
		t.Fatalf("missing provenance: %+v", doc)
	}
	other, err := ConvertDocument(context.Background(), "md", input, converterTestConfig())
	if err != nil || !reflect.DeepEqual(doc, other) {
		t.Fatalf("conversion is not deterministic: %v", err)
	}
}

func TestConverterJSONNumbersNullAndRecordLocators(t *testing.T) {
	input := []byte(`[{"z":9007199254740993,"a":null,"negative":-0.00010,"exponent":1e+100},null,{}]`)
	doc, err := ConvertDocument(context.Background(), "json", input, converterTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Blocks) != 3 || doc.Blocks[0].Text != `{"a":null,"exponent":1e+100,"negative":-0.00010,"z":9007199254740993}` || doc.Blocks[1].Text != "null" {
		t.Fatalf("JSON values changed: %+v", doc.Blocks)
	}
	for i, locator := range []string{"json-pointer:/0", "json-pointer:/1", "json-pointer:/2"} {
		if doc.Blocks[i].Locator != locator || doc.Blocks[i].Ordinal != i {
			t.Fatalf("incorrect record provenance: %+v", doc.Blocks)
		}
	}
	for _, input := range []string{"null", "[]", "{}", `"test"`, "1"} {
		if _, err := ConvertDocument(context.Background(), "json", []byte(input), converterTestConfig()); err != nil {
			t.Fatalf("valid JSON %s rejected: %v", input, err)
		}
	}
}

func TestConverterJSONLGlobalBudgetAndSourceLines(t *testing.T) {
	cfg := converterTestConfig()
	doc, err := ConvertDocument(context.Background(), "ndjson", []byte("\n{\"id\":1}\n\nnull\n{\"id\":2}\n"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Blocks) != 3 || doc.Blocks[0].Locator != "line:2;json-pointer:" || doc.Blocks[1].Locator != "line:4;json-pointer:" {
		t.Fatalf("incorrect JSONL locators: %+v", doc.Blocks)
	}
	cfg.JSONMaxElements = 3
	if _, err := ConvertDocument(context.Background(), "jsonl", []byte("{\"id\":1}\n{\"id\":2}"), cfg); err == nil {
		t.Fatal("per-line budget bypassed the global JSONL limit")
	}
}

func TestConverterTablePreservesColumnsMultilineAndEmptyValues(t *testing.T) {
	input := []byte("id,note,note,empty\r\n001,\"first\r\nsecond, quoted \"\"word\"\"\",null,\r\n002,ok,true,x\r\n")
	doc, err := ConvertDocument(context.Background(), "csv", input, converterTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Blocks) != 2 || doc.Blocks[0].Locator != "record:2;line:2" || doc.Blocks[1].Locator != "record:3;line:4" {
		t.Fatalf("incorrect table locators: %+v", doc.Blocks)
	}
	var row convertedTableRow
	if err := json.Unmarshal([]byte(doc.Blocks[0].Text), &row); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(row.Columns, []string{"id", "note", "note", "empty"}) || !reflect.DeepEqual(row.Values, []string{"001", "first\nsecond, quoted \"word\"", "null", ""}) {
		t.Fatalf("table coerced types or lost duplicate columns: %+v", row)
	}
	tsv, err := ConvertDocument(context.Background(), "tsv", []byte("id\tnote\n001\tnull\n"), converterTestConfig())
	if err != nil || len(tsv.Blocks) != 1 || !strings.Contains(tsv.Blocks[0].Text, `"001","null"`) {
		t.Fatalf("TSV conversion failed: %+v, %v", tsv, err)
	}
}

func TestConverterChunkedTableRetainsRecordAndColumn(t *testing.T) {
	cfg := converterTestConfig()
	cfg.ChunkMaxChars, cfg.ChunkOverlapChars = 100, 10
	doc, err := ConvertDocument(context.Background(), "csv", []byte("id,description\n001,"+strings.Repeat("ação ", 80)), cfg)
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := ChunkConvertedDocument(context.Background(), doc, cfg)
	if err != nil || len(chunks) < 3 {
		t.Fatalf("large table row was not chunked: %v, %v", chunks, err)
	}
	for i, chunk := range chunks {
		if utf8.RuneCountInString(chunk.Text) > 100 || !utf8.ValidString(chunk.Text) || chunk.CanonicalHash != sha256Hex([]byte(chunk.Text)) || !strings.Contains(chunk.Locator, "record:2;line:2;column:") {
			t.Fatalf("chunk exceeds budget or lacks provenance: %+v", chunk)
		}
		if i > 0 && !strings.Contains(chunk.Text, `column 2 "description"`) {
			t.Fatalf("column context lost: %+v", chunk)
		}
	}
}

func TestConverterRejectsIncompleteInvalidAndOversizedSources(t *testing.T) {
	for _, tc := range []struct {
		name, format, content string
		configure             func(*Config)
	}{
		{"unsupported binary adapter", "pdf", "%PDF-1.4", nil},
		{"binary", "txt", "hello\x00world", nil},
		{"binary beyond prefix", "txt", strings.Repeat("a", 1024) + "\x00", nil},
		{"UTF8", "txt", "hello\xff", nil},
		{"empty", "txt", " \n", nil},
		{"duplicate JSON keys", "json", `{"x":1,"x":2}`, nil},
		{"JSON trailing value", "json", "{} {}", nil},
		{"JSONL malformed later record", "jsonl", "{}\n{", nil},
		{"CSV malformed quote", "csv", "a,b\n1,\"no", nil},
		{"CSV wrong width", "csv", "a,b\n1,2,3", nil},
		{"CSV no records", "csv", "a,b\n", nil},
		{"bytes", "txt", "12345", func(c *Config) { c.MaxFileSize = 4 }},
		{"chunks", "txt", "123456789", func(c *Config) { c.ChunkMaxChars, c.ChunkOverlapChars, c.MaxChunksPerFile = 4, 0, 1 }},
		{"blocks", "json", "[1,2]", func(c *Config) { c.MaxChunksPerFile = 1 }},
		{"depth", "json", "[[1]]", func(c *Config) { c.JSONMaxDepth = 2 }},
		{"table global elements", "csv", "a,b\n1,2\n3,4", func(c *Config) { c.JSONMaxElements = 8 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := converterTestConfig()
			if tc.configure != nil {
				tc.configure(&cfg)
			}
			if doc, err := ConvertDocument(context.Background(), tc.format, []byte(tc.content), cfg); err == nil || len(doc.Blocks) != 0 {
				t.Fatalf("invalid input returned a publishable document: %+v, %v", doc, err)
			}
		})
	}
}

func TestConverterEnvelopeRoundTripStrictAndNoOrdinaryJSONCollision(t *testing.T) {
	cfg := converterTestConfig()
	doc, err := ConvertDocument(context.Background(), "json", []byte(`{"status":"not authorized"}`), cfg)
	if err != nil {
		t.Fatal(err)
	}
	doc.ProgramID, doc.Classification, doc.DocumentType, doc.Source = "acme", "restricted", "evidence", "capture.json"
	encoded, _ := json.Marshal(doc)
	decoded, err := DecodeConvertedDocument(encoded, cfg)
	if err != nil || !reflect.DeepEqual(doc, *decoded) {
		t.Fatalf("envelope did not round-trip: %+v, %v", decoded, err)
	}
	for _, input := range []string{`{"blocks":[]}`, "[1,2]", "null", "1"} {
		if decoded, err := DecodeConvertedDocument([]byte(input), cfg); err != nil || decoded != nil {
			t.Fatalf("ordinary JSON was treated as an envelope: %s: %v", input, err)
		}
	}
	for _, mutate := range []func(map[string]any){
		func(m map[string]any) { m["hive_document_schema"] = "hive-document/v2" },
		func(m map[string]any) { m["extraction_status"] = "partial" },
		func(m map[string]any) { m["raw_hash"] = "invalid" },
		func(m map[string]any) { m["blocks"].([]any)[0].(map[string]any)["ordinal"] = 2 },
		func(m map[string]any) { m["blocks"].([]any)[0].(map[string]any)["locator"] = "" },
		func(m map[string]any) { m["blocks"].([]any)[0].(map[string]any)["kind"] = "text" },
	} {
		var altered map[string]any
		_ = json.Unmarshal(encoded, &altered)
		mutate(altered)
		invalid, _ := json.Marshal(altered)
		if _, err := DecodeConvertedDocument(invalid, cfg); err == nil {
			t.Fatalf("malformed envelope accepted: %s", invalid)
		}
	}
}

func TestConverterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := converterTestConfig()
	if _, err := ConvertDocument(ctx, "txt", []byte("hello"), cfg); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled extraction continued: %v", err)
	}
	doc, err := ConvertDocument(context.Background(), "txt", []byte("hello"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ChunkConvertedDocument(ctx, doc, cfg); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled chunking continued: %v", err)
	}
}
