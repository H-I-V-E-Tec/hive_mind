package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// AuditEvent deliberately has no free-form message, query, payload or token.
type AuditEvent struct {
	Time           string `json:"time"`
	Session        string `json:"session"`
	PID            int    `json:"pid"`
	Version        string `json:"version"`
	SourceRevision string `json:"source_revision"`
	Hive           string `json:"hive_id"`
	Device         string `json:"device_id"`
	Role           string `json:"role"`
	Action         string `json:"action"`
	Outcome        string `json:"outcome"`
	Program        string `json:"program_id,omitempty"`
	Document       string `json:"document_id,omitempty"`
	Revision       string `json:"revision,omitempty"`
	Path           string `json:"path,omitempty"`
	ControlVersion int64  `json:"control_version,omitempty"`
	Count          int    `json:"count,omitempty"`
	DurationMS     int64  `json:"duration_ms,omitempty"`
	FilterHash     string `json:"filter_hash,omitempty"`
	ChangeID       string `json:"change_id,omitempty"`
	// Usage metrics for retrieval tools: size delivered to the agent versus
	// size of the source documents it would otherwise have to read. Numbers
	// only; they never carry text.
	ResponseChars int64 `json:"response_chars,omitempty"`
	SourceBytes   int64 `json:"source_bytes,omitempty"`
	Truncated     bool  `json:"truncated,omitempty"`
}

type AuditSink interface{ Record(AuditEvent) error }

type FileAudit struct {
	mu        sync.Mutex
	root      *os.Root
	name      string
	maxBytes  int64
	retention time.Duration
	session   string
	cfg       Config
	failed    atomic.Bool
}

// A separate file per process avoids competing append/rotation operations.
// The directory is outside the indexed tree and only accessible to its owner.
func OpenFileAudit(cfg Config) (*FileAudit, error) {
	fail := errors.New("audit storage is unavailable or insecure")
	if cfg.AuditDirectory == "" {
		return nil, fail
	}
	if err := os.MkdirAll(cfg.AuditDirectory, 0700); err != nil {
		return nil, fail
	}
	real, err := filepath.EvalSymlinks(cfg.AuditDirectory)
	if err != nil {
		return nil, fail
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return nil, fail
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() || !auditModeIsPrivate(info) {
		return nil, fail
	}
	if cfg.DataDirectory != "" {
		dataReal, err := filepath.EvalSymlinks(cfg.DataDirectory)
		if err != nil {
			return nil, fail
		}
		dataReal, err = filepath.Abs(dataReal)
		if err != nil {
			return nil, fail
		}
		rel, err := filepath.Rel(dataReal, real)
		if err != nil || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return nil, fail
		}
	}
	root, err := os.OpenRoot(real)
	if err != nil {
		return nil, fail
	}
	if err := secureAuditDirectory(root, real); err != nil {
		root.Close()
		return nil, fail
	}
	session := uuid.NewString()
	a := &FileAudit{root: root, name: "audit-" + session + ".jsonl", session: session, cfg: cfg, maxBytes: 10 << 20, retention: 30 * 24 * time.Hour}
	if err := a.Record(AuditEvent{Action: "process", Outcome: "started"}); err != nil {
		root.Close()
		return nil, fail
	}
	return a, nil
}

func (a *FileAudit) Record(e AuditEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	err := a.record(e)
	if err != nil {
		a.failed.Store(true)
		return errors.New("audit write failed")
	}
	return nil
}

func (a *FileAudit) record(e AuditEvent) error {
	if a.failed.Load() {
		return errors.New("audit previously failed")
	}
	e.Time, e.Session, e.PID = time.Now().UTC().Format(time.RFC3339Nano), a.session, os.Getpid()
	e.Version, e.Hive, e.Device, e.Role = Version, a.cfg.HiveID, a.cfg.DeviceID, a.cfg.Role
	e.SourceRevision = SourceRevision
	if !validIdentifier(e.Action, 64) || !validIdentifier(e.Outcome, 64) {
		return errors.New("invalid audit event")
	}
	if e.Program != "" && !validIdentifier(e.Program, 64) {
		e.Program = ""
	}
	if e.ChangeID != "" && !validIdentifier(e.ChangeID, 64) {
		return errors.New("invalid change identifier")
	}
	if e.Path != "" && (filepath.IsAbs(e.Path) || strings.Contains(e.Path, "\\") || strings.Contains(e.Path, "..") || containsControl(e.Path)) {
		e.Path = ""
	}
	for _, field := range []*string{&e.Document, &e.Revision, &e.FilterHash} {
		if len(*field) > 128 || strings.IndexFunc(*field, func(r rune) bool { return !strings.ContainsRune("0123456789abcdef-", r) }) >= 0 {
			*field = ""
		}
	}
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if info, err := a.root.Lstat(a.name); err == nil {
		if !info.Mode().IsRegular() || !auditModeIsPrivate(info) {
			return errors.New("unsafe audit file")
		}
		if info.Size()+int64(len(body)) > a.maxBytes {
			if err := a.root.Rename(a.name, "audit-"+a.session+"-"+uuid.NewString()+".jsonl"); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := a.root.OpenFile(a.name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if err := secureAuditFile(f, filepath.Join(a.root.Name(), a.name)); err != nil {
		_ = f.Close()
		return err
	}
	_, err = f.Write(body)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return a.expire()
}

func (a *FileAudit) expire() error {
	dir, err := a.root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == a.name || !strings.HasPrefix(name, "audit-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && time.Since(info.ModTime()) > a.retention {
			if err := a.root.Remove(name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *FileAudit) Close() error {
	err := a.Record(AuditEvent{Action: "process", Outcome: "stopped"})
	a.mu.Lock()
	defer a.mu.Unlock()
	closeErr := a.root.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (iw *IngestionWorker) audit(e AuditEvent, critical bool) error {
	if iw.Audit == nil || iw.auditFailed.Load() {
		iw.auditFailed.Store(true)
	} else if err := iw.Audit.Record(e); err != nil {
		iw.auditFailed.Store(true)
	} else {
		return nil
	}
	if critical {
		return &operationalError{ExitPartialFailure, "audit unavailable; critical operation blocked"}
	}
	return nil
}

func (iw *IngestionWorker) auditControl(recordType string, payload map[string]any, outcome string) error {
	get := func(k string) string { v, _ := payload[k].(string); return v }
	revision := get("active_document_revision")
	if revision == "" {
		revision = get("scope_revision")
	}
	version, _ := payload["control_version"].(int64)
	return iw.audit(AuditEvent{Action: recordType, Outcome: outcome, Program: get("program_id"), Document: get("document_id"), Path: get("path"), Revision: revision, ControlVersion: version}, true)
}

// Legacy diagnostics carry no free-form text into logs. Audited operations
// provide the structured detail; failures remain visible through status.
type privateDiagnosticWriter struct{ worker *IngestionWorker }

func (w privateDiagnosticWriter) Write(p []byte) (int, error) {
	_ = w.worker.audit(AuditEvent{Action: "diagnostic", Outcome: "recorded"}, false)
	return len(p), nil
}
