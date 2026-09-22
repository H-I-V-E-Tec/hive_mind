package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type convertOptions struct {
	Input, Output, Format, Program, Classification, DocumentType, Source, CollectedAt string
	Tags, AssetRefs                                                                   []string
	Ingest                                                                            bool
	Limits                                                                            Config
}

func defaultConversionLimits() Config {
	return Config{MaxFileSize: 5 << 20, MaxChunksPerFile: 1000, ChunkMaxChars: 2000,
		ChunkOverlapChars: 200, JSONMaxDepth: 64, JSONMaxElements: 100000}
}

// Conversion is deliberately offline: no credentials, service configuration or
// automatic network fetches are needed to inspect and prepare an import.
func parseConvertArgs(args []string) (convertOptions, error) {
	opts := convertOptions{DocumentType: "note", Limits: defaultConversionLimits()}
	seen := map[string]bool{}
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			if opts.Input != "" {
				return opts, errors.New("convert accepts one input path or -")
			}
			opts.Input = arg
			continue
		}
		if arg == "--ingest" {
			if seen[arg] {
				return opts, errors.New("duplicate convert option")
			}
			seen[arg] = true
			opts.Ingest = true
			continue
		}
		key, value, ok := strings.Cut(arg, "=")
		if !ok || value == "" {
			return opts, errors.New("convert options require --name=value")
		}
		if seen[key] && key != "--tag" && key != "--asset-ref" {
			return opts, errors.New("duplicate convert option")
		}
		seen[key] = true
		switch key {
		case "--output":
			opts.Output = value
		case "--format":
			opts.Format = strings.ToLower(strings.TrimPrefix(value, "."))
		case "--program":
			opts.Program = value
		case "--classification":
			opts.Classification = value
		case "--document-type":
			opts.DocumentType = value
		case "--source":
			opts.Source = value
		case "--collected-at":
			opts.CollectedAt = value
		case "--tag":
			opts.Tags = append(opts.Tags, value)
		case "--asset-ref":
			opts.AssetRefs = append(opts.AssetRefs, value)
		case "--max-file-bytes":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 1 || n > 50<<20 {
				return opts, errors.New("max-file-bytes must be between 1 and 52428800")
			}
			opts.Limits.MaxFileSize = n
		default:
			return opts, errors.New("unsupported convert option")
		}
	}
	if opts.Input == "" || !validIdentifier(opts.Program, 64) ||
		(opts.Classification != "internal" && opts.Classification != "restricted") ||
		!allowedDocumentTypes[opts.DocumentType] {
		return opts, errors.New("convert requires input, --program and --classification=internal|restricted")
	}
	if opts.Format == "" && opts.Input != "-" {
		opts.Format = strings.ToLower(strings.TrimPrefix(filepath.Ext(opts.Input), "."))
	}
	if !supportedDocumentExtension("." + opts.Format) {
		return opts, errors.New("unsupported format; use md, txt, json, jsonl, ndjson, csv or tsv")
	}
	if opts.Source == "" {
		if opts.Input == "-" {
			return opts, errors.New("stdin requires --source and --format")
		}
		opts.Source = filepath.Base(opts.Input)
	}
	if containsControl(opts.Source) || len(opts.Source) > 1024 {
		return opts, errors.New("invalid source label")
	}
	if opts.Output != "" && strings.ToLower(filepath.Ext(opts.Output)) != ".json" {
		return opts, errors.New("output must have a .json extension")
	}
	// --ingest publishes the envelope through the writer pipeline, so it needs a
	// real destination file under HIVE_DATA_DIR (checked once the config loads),
	// never stdout.
	if opts.Ingest && opts.Output == "" {
		return opts, errors.New("--ingest requires --output inside HIVE_DATA_DIR/programs/<program_id>/")
	}
	return opts, nil
}

func runConvert(ctx context.Context, opts convertOptions, stdin io.Reader) ([]byte, error) {
	var input io.Reader = stdin
	if opts.Input != "-" {
		info, err := os.Stat(opts.Input)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("input must be a readable regular file")
		}
		if info.Size() > opts.Limits.MaxFileSize {
			return nil, &documentInputError{"file_size_limit", "input exceeds max-file-bytes"}
		}
		file, err := os.Open(opts.Input)
		if err != nil {
			return nil, errors.New("input cannot be opened")
		}
		defer file.Close()
		input = file
	}
	content, err := io.ReadAll(io.LimitReader(input, opts.Limits.MaxFileSize+1))
	if err != nil {
		return nil, errors.New("input cannot be read")
	}
	if int64(len(content)) > opts.Limits.MaxFileSize {
		return nil, &documentInputError{"file_size_limit", "input exceeds max-file-bytes"}
	}
	doc, err := ConvertDocument(ctx, opts.Format, content, opts.Limits)
	if err != nil {
		return nil, err
	}
	// Authority is supplied explicitly by the operator, never imported from a
	// CSV column, a JSON property or Markdown front matter in the source.
	doc.ProgramID, doc.Classification = opts.Program, opts.Classification
	doc.DocumentType, doc.Source, doc.CollectedAt = opts.DocumentType, opts.Source, opts.CollectedAt
	doc.Tags, doc.AssetRefs = opts.Tags, opts.AssetRefs
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, errors.New("converted document cannot be encoded")
	}
	encoded = append(encoded, '\n')
	if int64(len(encoded)) > opts.Limits.MaxFileSize {
		return nil, &documentInputError{"file_size_limit", "converted envelope exceeds max-file-bytes; split the input or raise the limit for conversion and ingestion"}
	}
	if _, err := extractReconMetadata("programs/"+opts.Program+"/import.json", opts.Program, encoded); err != nil {
		return nil, errors.New("invalid conversion metadata; check collected-at, tags and asset-ref")
	}
	if _, err := DecodeConvertedDocument(encoded, opts.Limits); err != nil {
		return nil, err
	}
	if _, err := ChunkConvertedDocument(ctx, doc, opts.Limits); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return encoded, nil
}

// Publish a complete envelope without clobbering an existing source. The
// temporary file is hidden so an active watcher cannot ingest partial output.
func writeConvertedDocument(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".hive-convert-*")
	if err != nil {
		return errors.New("output directory is not writable")
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return errors.New("converted output could not be written")
	}
	if err := file.Sync(); err != nil {
		return errors.New("converted output could not be synchronized")
	}
	if err := file.Close(); err != nil {
		return errors.New("converted output could not be closed")
	}
	if err := os.Link(file.Name(), path); err != nil {
		return errors.New("output could not be published; destination must not exist and must support hard links")
	}
	return nil
}

func conversionErrorMessage(err error) string {
	var inputErr *documentInputError
	if errors.As(err, &inputErr) {
		return fmt.Sprintf("conversion failed (%s): %s", inputErr.reason, syncReasonDetail(inputErr.reason))
	}
	if errors.Is(err, context.Canceled) {
		return "conversion cancelled"
	}
	// Parser errors may contain input fragments. Never print them to stderr.
	return "conversion failed; check input format, limits, metadata and output destination"
}
