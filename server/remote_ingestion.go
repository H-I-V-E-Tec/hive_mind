package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	remoteContentLimit   = 16 << 10
	remoteRequestLimit   = 128 << 10 // JSON escapes can expand each content byte sixfold.
	remoteIngestTimeout  = 45 * time.Second
	permissionMindRead   = "product.mind"
	permissionMindIngest = "mind.ingest"
)

type HiveIngestDocumentArguments struct {
	ProgramID      string `json:"program_id"`
	Classification string `json:"classification"`
	DocumentType   string `json:"document_type"`
	SourceFormat   string `json:"source_format"`
	Content        string `json:"content"`
	Platform       string `json:"platform,omitempty"`
	TargetName     string `json:"target_name,omitempty"`
}

func validateRemoteDocument(args HiveIngestDocumentArguments) error {
	if !validIdentifier(args.ProgramID, 64) ||
		(args.Classification != "internal" && args.Classification != "restricted") ||
		(args.DocumentType != "note" && args.DocumentType != "evidence") ||
		(args.SourceFormat != "txt" && args.SourceFormat != "md") {
		return errors.New("provide a valid program_id, classification, document_type and source_format")
	}
	if len(args.Content) > remoteContentLimit {
		return &documentInputError{"file_size_limit", "content exceeds 16384 UTF-8 bytes"}
	}
	if strings.TrimSpace(args.Content) == "" || !utf8.ValidString(args.Content) || isBinaryContent([]byte(args.Content)) {
		return errors.New("content must be nonempty UTF-8 text without binary data")
	}
	if (args.Platform == "") != (args.TargetName == "") ||
		(args.Platform != "" && (!validIdentifier(args.Platform, 64) || !validTargetName(args.TargetName, args.DocumentType))) {
		return errors.New("platform and target_name must be valid and provided together")
	}
	return nil
}

func failedDocumentReport(code int, message string) IngestionReport {
	return IngestionReport{SchemaVersion: 1, ExitCode: code, Error: message,
		Summary: SyncSummary{Results: []SyncFileResult{}}}
}

// The HTTP boundary supplies verified claims. The caller never supplies authorship.
func (iw *IngestionWorker) IngestRemoteDocument(ctx context.Context, claims *HiveClaims, args HiveIngestDocumentArguments) IngestionReport {
	if !iw.Cfg.IsWriter() || claims == nil || !claims.HasPermissions(permissionMindRead, permissionMindIngest) || !validMemberSubject(claims.Sub) {
		return failedDocumentReport(ExitAuthorization, "document ingestion requires an authorized member and writer")
	}
	if err := validateRemoteDocument(args); err != nil {
		return failedDocumentReport(ExitUsage, err.Error())
	}
	doc, err := ConvertDocument(ctx, args.SourceFormat, []byte(args.Content), iw.Cfg)
	if err != nil {
		return failedDocumentReport(ExitUsage, conversionErrorMessage(err))
	}
	doc.ProgramID, doc.Classification, doc.DocumentType = args.ProgramID, args.Classification, args.DocumentType
	doc.Platform, doc.TargetName = args.Platform, args.TargetName
	doc.Source = "remote-member:" + claims.Sub
	encoded, err := json.Marshal(doc)
	if err != nil || int64(len(encoded)) > iw.Cfg.MaxFileSize {
		return failedDocumentReport(ExitUsage, "converted document exceeds the configured limits")
	}
	if _, err := DecodeConvertedDocument(encoded, iw.Cfg); err != nil {
		return failedDocumentReport(ExitUsage, "invalid converted document")
	}
	if _, err := ChunkConvertedDocument(ctx, doc, iw.Cfg); err != nil {
		return failedDocumentReport(ExitUsage, "document exceeds the configured chunk limits")
	}
	request, _ := json.Marshal(args)
	rel := filepath.Join("programs", args.ProgramID, "imports", "remote", sha256Hex([]byte(claims.Sub)), sha256Hex(request)+".json")
	if err := ctx.Err(); err != nil {
		return failedDocumentReport(ExitPartialFailure, syncReasonDetail("cancelled"))
	}
	if err := publishRemoteDocument(iw.Cfg.DataDirectory, rel, encoded); err != nil {
		return failedDocumentReport(ExitPartialFailure, "document could not be saved safely; retry the same request after checking server storage")
	}
	report := iw.IngestPathReport(ctx, filepath.Join(iw.Cfg.DataDirectory, rel))
	if report.OK && (len(report.Summary.Results) != 1 ||
		(!report.Summary.Results[0].Published && report.Summary.Results[0].Outcome != SyncUnchanged)) {
		report.OK, report.ExitCode, report.Error = false, ExitPartialFailure, "document publication was not confirmed; inspect the file result"
	}
	return report
}

// Root confines all publication operations, including symlinks. Link exposes only
// complete bytes and refuses to replace an existing document. Hidden temps are
// ignored by the watcher and removed after publication.
func publishRemoteDocument(dataDir, rel string, encoded []byte) error {
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		return err
	}
	defer root.Close()
	parent := ""
	for _, component := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
		parent = filepath.Join(parent, component)
		info, err := root.Lstat(parent)
		if errors.Is(err, os.ErrNotExist) {
			if err := root.Mkdir(parent, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = root.Lstat(parent)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("document parent must be a real directory")
		}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(rel), ".hive-remote-"+hex.EncodeToString(nonce[:]))
	file, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp)
	defer file.Close()
	if err := secureConvertedFile(file); err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := root.Link(tmp, rel); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := root.Lstat(rel)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(encoded)) {
		return errors.New("existing document is not the expected regular file")
	}
	existing, err := root.Open(rel)
	if err != nil {
		return err
	}
	defer existing.Close()
	content, err := io.ReadAll(io.LimitReader(existing, int64(len(encoded))+1))
	if err != nil || !bytes.Equal(content, encoded) {
		return errors.New("existing document bytes differ")
	}
	return nil
}

func validMemberSubject(sub string) bool {
	return strings.TrimSpace(sub) != "" && len(sub) <= 256 && !containsControl(sub)
}

func (c *HiveClaims) HasPermissions(required ...string) bool {
	if c == nil {
		return false
	}
	for _, permission := range required {
		found := false
		for _, granted := range c.Permissions {
			if granted == permission {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
