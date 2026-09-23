package server

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const ConvertedDocumentSchema = "hive-document/v1"
const converterVersion = "hive-converter-v1"

// ConvertedDocument is the portable ingestion representation. Authorization
// metadata is supplied by the importing operator, never inferred by an adapter.
// It is not a canonical knowledge unit or a claim of global deduplication.
type ConvertedDocument struct {
	Schema               string           `json:"hive_document_schema"`
	ProgramID            string           `json:"program_id"`
	Platform             string           `json:"platform,omitempty"`
	TargetName           string           `json:"target_name,omitempty"`
	Classification       string           `json:"classification"`
	DocumentType         string           `json:"document_type"`
	Source               string           `json:"source"`
	CollectedAt          string           `json:"collected_at,omitempty"`
	Tags                 []string         `json:"tags,omitempty"`
	AssetRefs            []string         `json:"asset_refs,omitempty"`
	ObservedTargets      []string         `json:"observed_targets,omitempty"`
	SourceFormat         string           `json:"source_format"`
	RawHash              string           `json:"raw_hash"`
	ConverterFingerprint string           `json:"converter_fingerprint"`
	Blocks               []ConvertedBlock `json:"blocks"`
}

type ConvertedBlock struct {
	Kind    string `json:"kind"`
	Text    string `json:"text"`
	Locator string `json:"locator"`
	Ordinal int    `json:"ordinal"`
}

type ConvertedChunk struct {
	Text          string `json:"text"`
	BlockOrdinal  int    `json:"block_ordinal"`
	Locator       string `json:"locator"`
	CanonicalHash string `json:"canonical_hash"`
}

type convertedTableRow struct {
	Columns []string `json:"columns"`
	Values  []string `json:"values"`
}

func supportedDocumentExtension(ext string) bool {
	switch strings.TrimPrefix(strings.ToLower(ext), ".") {
	case "md", "txt", "json", "jsonl", "ndjson", "csv", "tsv":
		return true
	default:
		return false
	}
}

func validateConverterConfig(cfg Config) error {
	if cfg.MaxFileSize <= 0 || cfg.MaxFileSize > 50<<20 || cfg.MaxChunksPerFile <= 0 || cfg.MaxChunksPerFile > 5000 ||
		cfg.ChunkMaxChars <= 0 || cfg.ChunkMaxChars > 8000 || cfg.ChunkOverlapChars < 0 || cfg.ChunkOverlapChars > cfg.ChunkMaxChars/2 ||
		cfg.JSONMaxDepth <= 0 || cfg.JSONMaxDepth > 64 || cfg.JSONMaxElements <= 0 || cfg.JSONMaxElements > 100000 {
		return errors.New("invalid converter limits")
	}
	return nil
}

func converterInput(content []byte, cfg Config) ([]byte, error) {
	if err := validateConverterConfig(cfg); err != nil {
		return nil, err
	}
	if int64(len(content)) > cfg.MaxFileSize {
		return nil, &documentInputError{"file_size_limit", "conversion input exceeds the configured byte limit"}
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return nil, &documentInputError{"invalid_encoding", "conversion requires UTF-8 text without binary content"}
	}
	// Only transport normalization is implicit: preserve numbers, case,
	// whitespace inside values, negation, ordering and Unicode code points.
	normalized := strings.TrimPrefix(string(content), "\ufeff")
	normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	if strings.TrimSpace(normalized) == "" {
		return nil, &documentInputError{"empty_document", "conversion input is empty"}
	}
	return []byte(normalized), nil
}

// ConvertDocument extracts a complete deterministic representation without
// network access, filesystem access or document-supplied authorization.
func ConvertDocument(ctx context.Context, format string, content []byte, cfg Config) (ConvertedDocument, error) {
	var out ConvertedDocument
	if err := ctx.Err(); err != nil {
		return out, err
	}
	format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	if !supportedDocumentExtension(format) {
		return out, &documentInputError{"unsupported_format", "no complete adapter for the requested format"}
	}
	normalized, err := converterInput(content, cfg)
	if err != nil {
		return out, err
	}
	out = ConvertedDocument{Schema: ConvertedDocumentSchema, SourceFormat: format, RawHash: sha256Hex(content),
		ConverterFingerprint: fmt.Sprintf("%s:format=%s:json-depth=%d:json-elements=%d", converterVersion, format, cfg.JSONMaxDepth, cfg.JSONMaxElements),
		Blocks:               []ConvertedBlock{}}
	addBlock := func(kind, text, locator string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(out.Blocks) >= cfg.MaxChunksPerFile {
			return &documentInputError{"chunk_limit", "extracted blocks exceed the configured chunk limit"}
		}
		out.Blocks = append(out.Blocks, ConvertedBlock{Kind: kind, Text: text, Locator: locator, Ordinal: len(out.Blocks)})
		return nil
	}
	budget := cfg.JSONMaxElements
	switch format {
	case "txt", "md":
		kind := "text"
		if format == "md" {
			kind = "markdown"
		}
		err = addBlock(kind, string(normalized), fmt.Sprintf("lines:1-%d", bytes.Count(normalized, []byte("\n"))+1))
	case "json":
		var value any
		value, err = decodeBoundedJSON(ctx, normalized, cfg.JSONMaxDepth, &budget)
		if err != nil {
			break
		}
		// Array items are independent records. Objects stay intact so keys
		// and surrounding qualifiers are retained with their values.
		if array, ok := value.([]any); ok && len(array) > 0 {
			for i, item := range array {
				encoded, _ := json.Marshal(item)
				if err = addBlock("json", string(encoded), "json-pointer:/"+strconv.Itoa(i)); err != nil {
					break
				}
			}
		} else {
			encoded, _ := json.Marshal(value)
			err = addBlock("json", string(encoded), "json-pointer:")
		}
	case "jsonl", "ndjson":
		for lineNumber, line := range bytes.Split(normalized, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var value any
			value, err = decodeBoundedJSON(ctx, line, cfg.JSONMaxDepth, &budget)
			if err != nil {
				err = fmt.Errorf("JSON record at line %d: %w", lineNumber+1, err)
				break
			}
			encoded, _ := json.Marshal(value)
			if err = addBlock("json", string(encoded), fmt.Sprintf("line:%d;json-pointer:", lineNumber+1)); err != nil {
				break
			}
		}
	case "csv", "tsv":
		reader := csv.NewReader(bytes.NewReader(normalized))
		if format == "tsv" {
			reader.Comma = '\t'
		}
		var columns []string
		columns, err = reader.Read()
		if err != nil {
			err = errors.New("invalid table header")
			break
		}
		budget -= len(columns) + 1
		for record := 2; ; record++ {
			if err = ctx.Err(); err != nil {
				break
			}
			var values []string
			values, err = reader.Read()
			if err == io.EOF {
				err = nil
				break
			}
			if err != nil {
				err = fmt.Errorf("invalid table record %d", record)
				break
			}
			budget -= len(values) + 1
			if budget < 0 {
				err = &documentInputError{"invalid_document", "table exceeds the configured element limit"}
				break
			}
			line, _ := reader.FieldPos(0)
			encoded, _ := json.Marshal(convertedTableRow{Columns: columns, Values: values})
			if err = addBlock("table_row", string(encoded), fmt.Sprintf("record:%d;line:%d", record, line)); err != nil {
				break
			}
		}
	}
	if err != nil {
		return ConvertedDocument{}, err
	}
	if len(out.Blocks) == 0 {
		return ConvertedDocument{}, &documentInputError{"empty_document", "input contains no data records"}
	}
	// Reject work which cannot fit before returning a publishable envelope.
	if _, err := ChunkConvertedDocument(ctx, out, cfg); err != nil {
		return ConvertedDocument{}, err
	}
	return out, nil
}

// decodeBoundedJSON enforces depth/elements while reading, preserves exact
// number lexemes and rejects duplicate keys instead of silently losing values.
func decodeBoundedJSON(ctx context.Context, content []byte, maxDepth int, remaining *int) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var readValue func(int) (any, error)
	readValue = func(depth int) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if depth > maxDepth || *remaining <= 0 {
			return nil, &documentInputError{"invalid_document", "JSON exceeds the configured depth or element limit"}
		}
		*remaining--
		token, err := decoder.Token()
		if err != nil {
			return nil, errors.New("invalid JSON value")
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return token, nil
		}
		switch delim {
		case '{':
			object := make(map[string]any)
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, errors.New("invalid JSON object key")
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("invalid JSON object key")
				}
				if _, exists := object[key]; exists {
					return nil, errors.New("duplicate JSON object key")
				}
				value, err := readValue(depth + 1)
				if err != nil {
					return nil, err
				}
				object[key] = value
			}
			if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
				return nil, errors.New("invalid JSON object")
			}
			return object, nil
		case '[':
			array := make([]any, 0)
			for decoder.More() {
				value, err := readValue(depth + 1)
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			if closing, err := decoder.Token(); err != nil || closing != json.Delim(']') {
				return nil, errors.New("invalid JSON array")
			}
			return array, nil
		default:
			return nil, errors.New("invalid JSON delimiter")
		}
	}
	value, err := readValue(1)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("invalid JSON: trailing value")
	}
	return value, nil
}

// DecodeConvertedDocument distinguishes the reserved envelope from ordinary
// JSON. Unknown fields/schema, partial extraction and malformed blocks fail
// closed. Authorization metadata is validated by the ingestion boundary.
func DecodeConvertedDocument(content []byte, cfg Config) (*ConvertedDocument, error) {
	if err := validateConverterConfig(cfg); err != nil {
		return nil, err
	}
	if int64(len(content)) > cfg.MaxFileSize {
		return nil, &documentInputError{"file_size_limit", "converted document exceeds the configured byte limit"}
	}
	var marker map[string]json.RawMessage
	if err := json.Unmarshal(content, &marker); err != nil {
		// Arrays and scalars remain ordinary JSON; the existing parser will
		// validate them, including depth and element limits.
		return nil, nil
	}
	if _, ok := marker["hive_document_schema"]; !ok {
		return nil, nil
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return nil, &documentInputError{"invalid_encoding", "converted document must be UTF-8 text"}
	}
	// Envelope scaffolding has its own bounded allowance. Content limits are
	// checked separately below so large block arrays cannot bypass source caps.
	budget := cfg.JSONMaxElements + cfg.MaxChunksPerFile*8 + 32
	if _, err := decodeBoundedJSON(context.Background(), content, 12, &budget); err != nil {
		return nil, err
	}
	var out ConvertedDocument
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return nil, errors.New("invalid converted document fields")
	}
	if err := validateConvertedDocument(context.Background(), out, cfg); err != nil {
		return nil, err
	}
	if _, err := ChunkConvertedDocument(context.Background(), out, cfg); err != nil {
		return nil, err
	}
	return &out, nil
}

func validateConvertedDocument(ctx context.Context, doc ConvertedDocument, cfg Config) error {
	if doc.Schema != ConvertedDocumentSchema || !supportedDocumentExtension(doc.SourceFormat) || strings.HasPrefix(doc.SourceFormat, ".") || doc.SourceFormat != strings.ToLower(doc.SourceFormat) {
		return errors.New("unsupported converted document schema or source format")
	}
	if !strings.HasPrefix(doc.ConverterFingerprint, converterVersion+":") {
		return errors.New("unsupported converter fingerprint")
	}
	if hash, err := hex.DecodeString(doc.RawHash); err != nil || len(hash) != 32 || strings.ToLower(doc.RawHash) != doc.RawHash {
		return errors.New("converted document requires a SHA-256 raw_hash")
	}
	if len(doc.Blocks) == 0 {
		return &documentInputError{"empty_document", "converted document contains no blocks"}
	}
	if len(doc.Blocks) > cfg.MaxChunksPerFile {
		return &documentInputError{"chunk_limit", "converted document has too many blocks"}
	}
	var totalBytes int64
	budget := cfg.JSONMaxElements
	if doc.SourceFormat == "json" && len(doc.Blocks) > 1 {
		budget-- // the source array containing the independently stored records
	}
	var tableColumns []string
	for ordinal, block := range doc.Blocks {
		if err := ctx.Err(); err != nil {
			return err
		}
		if block.Ordinal != ordinal || strings.TrimSpace(block.Locator) == "" || len(block.Locator) > 1024 || strings.TrimSpace(block.Text) == "" {
			return errors.New("converted blocks require ordered ordinals, text and a locator")
		}
		if !utf8.ValidString(block.Text) || strings.IndexByte(block.Text, 0) >= 0 {
			return &documentInputError{"invalid_encoding", "converted blocks contain binary or invalid UTF-8 text"}
		}
		totalBytes += int64(len(block.Text))
		if totalBytes > cfg.MaxFileSize {
			return &documentInputError{"file_size_limit", "converted block text exceeds the configured byte limit"}
		}
		switch block.Kind {
		case "text", "markdown":
			if (block.Kind == "text" && doc.SourceFormat != "txt") || (block.Kind == "markdown" && doc.SourceFormat != "md") {
				return errors.New("converted block kind does not match its source format")
			}
		case "json":
			if doc.SourceFormat != "json" && doc.SourceFormat != "jsonl" && doc.SourceFormat != "ndjson" {
				return errors.New("converted JSON block does not match its source format")
			}
			if _, err := decodeBoundedJSON(ctx, []byte(block.Text), cfg.JSONMaxDepth, &budget); err != nil {
				return err
			}
		case "table_row":
			if doc.SourceFormat != "csv" && doc.SourceFormat != "tsv" {
				return errors.New("converted table block does not match its source format")
			}
			var row convertedTableRow
			rowBudget := cfg.JSONMaxElements*2 + 3
			if _, err := decodeBoundedJSON(ctx, []byte(block.Text), 3, &rowBudget); err != nil {
				return err
			}
			decoder := json.NewDecoder(strings.NewReader(block.Text))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&row); err != nil || len(row.Columns) == 0 || len(row.Columns) != len(row.Values) {
				return errors.New("invalid converted table row")
			}
			var trailing any
			if err := decoder.Decode(&trailing); err != io.EOF {
				return errors.New("invalid converted table row: trailing value")
			}
			if ordinal == 0 {
				tableColumns = row.Columns
			} else if strings.Join(tableColumns, "\x00") != strings.Join(row.Columns, "\x00") {
				return errors.New("converted table rows must retain their header")
			}
			for i, value := range row.Values {
				if strings.IndexByte(value, 0) >= 0 || strings.IndexByte(row.Columns[i], 0) >= 0 {
					return errors.New("converted table contains binary cell content")
				}
			}
			budget -= len(row.Values) + 1
			if ordinal == 0 {
				budget -= len(row.Columns) + 1
			}
			if budget < 0 {
				return errors.New("converted table exceeds the configured element limit")
			}
		default:
			return errors.New("unsupported or partial extracted block")
		}
	}
	return nil
}

// ChunkConvertedDocument creates bounded retrieval projections. Rune offsets
// are zero-based and end-exclusive within the block (or table cell). The hash
// identifies projection content only; it does not grant deduplication rights.
func ChunkConvertedDocument(ctx context.Context, doc ConvertedDocument, cfg Config) ([]ConvertedChunk, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateConverterConfig(cfg); err != nil {
		return nil, err
	}
	if err := validateConvertedDocument(ctx, doc, cfg); err != nil {
		return nil, err
	}
	chunks := make([]ConvertedChunk, 0)
	addText := func(block ConvertedBlock, prefix, text, locator string) error {
		capacity := cfg.ChunkMaxChars - utf8.RuneCountInString(prefix)
		if capacity < 1 {
			return &documentInputError{"chunk_limit", "chunk size cannot retain the table column context"}
		}
		overlap := cfg.ChunkOverlapChars
		if overlap >= capacity {
			overlap = capacity / 2
		}
		runes := []rune(text)
		for start := 0; start < len(runes) || (start == 0 && len(runes) == 0); start += capacity - overlap {
			if err := ctx.Err(); err != nil {
				return err
			}
			if len(chunks) >= cfg.MaxChunksPerFile {
				return &documentInputError{"chunk_limit", "converted document exceeds the configured chunk limit"}
			}
			end := start + capacity
			if end > len(runes) {
				end = len(runes)
			}
			value := prefix + string(runes[start:end])
			chunks = append(chunks, ConvertedChunk{Text: value, BlockOrdinal: block.Ordinal,
				Locator: fmt.Sprintf("%s;runes:%d-%d", locator, start, end), CanonicalHash: sha256Hex([]byte(value))})
			if end == len(runes) {
				break
			}
		}
		return nil
	}
	for _, block := range doc.Blocks {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if block.Kind == "table_row" && utf8.RuneCountInString(block.Text) > cfg.ChunkMaxChars {
			var row convertedTableRow
			if err := json.Unmarshal([]byte(block.Text), &row); err != nil {
				return nil, errors.New("invalid converted table row")
			}
			for i, value := range row.Values {
				locator := fmt.Sprintf("%s;column:%d", block.Locator, i+1)
				prefix := fmt.Sprintf("%s; column %d %s\n", block.Locator, i+1, strconv.Quote(row.Columns[i]))
				if err := addText(block, prefix, value, locator); err != nil {
					return nil, err
				}
			}
		} else if err := addText(block, "", block.Text, block.Locator); err != nil {
			return nil, err
		}
	}
	return chunks, nil
}
