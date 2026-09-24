package server

import (
	"bufio"
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
	Input, Output, Format, Program, Platform, TargetName, Classification, DocumentType, Source, CollectedAt string
	Tags, AssetRefs, ObservedTargets                                                                        []string
	Ingest                                                                                                  bool
	Interactive                                                                                             bool
	DocumentTypeSpecified                                                                                   bool
	Limits                                                                                                  Config
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
		if arg == "--interactive" {
			if seen[arg] {
				return opts, errors.New("duplicate convert option")
			}
			seen[arg] = true
			opts.Interactive = true
			continue
		}
		key, value, ok := strings.Cut(arg, "=")
		if !ok || value == "" {
			return opts, errors.New("convert options require --name=value")
		}
		if seen[key] && key != "--tag" && key != "--asset-ref" && key != "--observed-target" {
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
		case "--platform":
			opts.Platform = strings.ToLower(value)
		case "--target":
			opts.TargetName = value
		case "--classification":
			opts.Classification = value
		case "--document-type":
			opts.DocumentType = value
			opts.DocumentTypeSpecified = true
		case "--source":
			opts.Source = value
		case "--collected-at":
			opts.CollectedAt = value
		case "--tag":
			opts.Tags = append(opts.Tags, value)
		case "--asset-ref":
			opts.AssetRefs = append(opts.AssetRefs, value)
		case "--observed-target":
			opts.ObservedTargets = append(opts.ObservedTargets, value)
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
	if opts.Input == "" || !allowedDocumentTypes[opts.DocumentType] ||
		(opts.Program != "" && !validIdentifier(opts.Program, 64)) ||
		(opts.Platform != "" && !validIdentifier(opts.Platform, 64)) ||
		(opts.Classification != "" && opts.Classification != "internal" && opts.Classification != "restricted") {
		return opts, errors.New("invalid conversion metadata")
	}
	if opts.Interactive && (!opts.Ingest || opts.Input == "-") {
		return opts, errors.New("--interactive requires --ingest and a file input")
	}
	if opts.Ingest && opts.DocumentTypeSpecified && (opts.DocumentType == "scope" || opts.DocumentType == "unknown") {
		return opts, errors.New("convert --ingest requires a concrete document type; scope.json is maintained separately")
	}
	if !opts.Interactive && (!validIdentifier(opts.Program, 64) ||
		(opts.Classification != "internal" && opts.Classification != "restricted")) {
		return opts, errors.New("convert requires input, --program and --classification=internal|restricted")
	}
	if opts.TargetName != "" && !(opts.Interactive && !opts.DocumentTypeSpecified) && !validTargetName(opts.TargetName, opts.DocumentType) {
		return opts, errors.New("--target must be a project name (or @program for scope/rules)")
	}
	if opts.Ingest && !opts.Interactive && (opts.Platform == "" || opts.TargetName == "" || !opts.DocumentTypeSpecified) {
		return opts, errors.New("convert --ingest requires --platform, --target and --document-type")
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
	if opts.Ingest && opts.Output == "" && !opts.Interactive {
		return opts, errors.New("--ingest requires --output inside HIVE_DATA_DIR/programs/<program_id>/")
	}
	if (opts.Platform == "") != (opts.TargetName == "") && !opts.Interactive {
		return opts, errors.New("--platform and --target must be provided together")
	}
	return opts, nil
}

// Prompts are an explicit CLI-only enrollment step. Watchers and MCP callers
// never read stdin and must submit already registered document metadata.
func completeInteractiveConvert(opts convertOptions, input io.Reader, output io.Writer) (convertOptions, error) {
	if !opts.Interactive {
		return opts, nil
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 4096)
	read := func(label string, dest *string) error {
		if *dest != "" {
			return nil
		}
		if _, err := fmt.Fprint(output, label+": "); err != nil {
			return err
		}
		if !scanner.Scan() {
			return errors.New("interactive registration cancelled or incomplete")
		}
		*dest = strings.TrimSpace(scanner.Text())
		if *dest == "" {
			return errors.New("interactive registration requires every field")
		}
		return nil
	}
	for _, field := range []struct {
		label string
		dest  *string
	}{
		{"Plataforma (h1, bugcrowd, ...)", &opts.Platform},
		{"Programa (id dentro da plataforma)", &opts.Program},
		{"Tipo do arquivo (asset, endpoint, note, evidence, rules)", &opts.DocumentType},
		{"Nome do alvo/projeto (@program para escopo/regras)", &opts.TargetName},
		{"Classificação (internal ou restricted)", &opts.Classification},
	} {
		if field.dest == &opts.DocumentType && !opts.DocumentTypeSpecified {
			opts.DocumentType = ""
		}
		if err := read(field.label, field.dest); err != nil {
			return opts, err
		}
		if field.dest == &opts.DocumentType {
			opts.DocumentTypeSpecified = true
		}
	}
	opts.Platform = strings.ToLower(opts.Platform)
	if !validIdentifier(opts.Platform, 64) || !validIdentifier(opts.Program, 64) ||
		(opts.Classification != "internal" && opts.Classification != "restricted") || !allowedDocumentTypes[opts.DocumentType] ||
		opts.DocumentType == "scope" || opts.DocumentType == "unknown" {
		return opts, errors.New("invalid registration; check platform, program and classification")
	}
	if !validTargetName(opts.TargetName, opts.DocumentType) {
		return opts, errors.New("invalid registration; target must be a project name")
	}
	return opts, nil
}

// Resolve the publication path before writing any data. The destination must
// remain under the selected program even when a parent is a symlink.
func prepareConvertIngestOutput(opts convertOptions) (convertOptions, error) {
	if !opts.Ingest {
		return opts, nil
	}
	cfg, err := LoadConfig()
	if err != nil {
		return opts, &operationalError{ExitConfiguration, "convert --ingest requires valid Hive configuration"}
	}
	if !cfg.IsWriter() {
		return opts, &operationalError{ExitAuthorization, "convert --ingest requires HIVE_ROLE=writer"}
	}
	programDir := filepath.Join(cfg.DataDirectory, "programs", opts.Program)
	if opts.Output == "" {
		if !opts.Interactive {
			return opts, errors.New("convert --ingest requires --output")
		}
		opts.Output = filepath.Join(programDir, filepath.Base(opts.Input)+".json")
	}
	abs, err := filepath.Abs(opts.Output)
	if err != nil || !pathWithin(programDir, abs) || abs == programDir || strings.ToLower(filepath.Ext(abs)) != ".json" {
		return opts, errors.New("output must be a .json file inside HIVE_DATA_DIR/programs/<program_id>/")
	}
	realProgram, err := filepath.EvalSymlinks(programDir)
	if err != nil {
		return opts, errors.New("program directory is unavailable; create it before importing")
	}
	realDataDir, err := filepath.EvalSymlinks(cfg.DataDirectory)
	if err != nil || !pathWithin(filepath.Join(realDataDir, "programs"), realProgram) {
		return opts, errors.New("program directory must stay inside HIVE_DATA_DIR")
	}
	realParent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil || !pathWithin(realProgram, realParent) {
		return opts, errors.New("output parent must stay inside the program directory")
	}
	opts.Output = abs
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
	doc.Platform = opts.Platform
	doc.TargetName = strings.TrimSpace(opts.TargetName)
	doc.DocumentType, doc.Source, doc.CollectedAt = opts.DocumentType, opts.Source, opts.CollectedAt
	doc.Tags, doc.AssetRefs = opts.Tags, opts.AssetRefs
	if doc.TargetName != "" && doc.TargetName != "@program" {
		doc.ObservedTargets, err = extractObservedTargets(doc, opts.ObservedTargets)
		if err != nil {
			return nil, err
		}
	}
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
	if err := secureConvertedFile(file); err != nil {
		return errors.New("converted output could not be secured")
	}
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
