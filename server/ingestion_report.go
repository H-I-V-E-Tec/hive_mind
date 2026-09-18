package server

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"
)

type SyncOutcome string

const (
	SyncCreated   SyncOutcome = "created"
	SyncUpdated   SyncOutcome = "updated"
	SyncUnchanged SyncOutcome = "unchanged"
	SyncSkipped   SyncOutcome = "skipped"
	SyncMissing   SyncOutcome = "missing"
	SyncFailed    SyncOutcome = "failed"
	SyncCancelled SyncOutcome = "cancelled"
)

// SyncFileResult never contains document text or raw provider errors. Published
// means publication was confirmed in this invocation; false is not proof that
// no remote write occurred (a commit acknowledgement may have been lost).
type SyncFileResult struct {
	Path       string      `json:"path,omitempty"`
	Outcome    SyncOutcome `json:"outcome"`
	Published  bool        `json:"published"`
	ReasonCode string      `json:"reason_code,omitempty"`
	Detail     string      `json:"detail,omitempty"`
	Warnings   []string    `json:"warnings,omitempty"`
}

// Total counts supported, non-ignored files in a completed scan. Ingested is
// retained for callers but now counts only successful creations and updates.
type SyncSummary struct {
	ScanComplete bool             `json:"scan_complete"`
	Total        int              `json:"total"`
	Ingested     int              `json:"ingested"`
	Created      int              `json:"created"`
	Updated      int              `json:"updated"`
	Unchanged    int              `json:"unchanged"`
	Skipped      int              `json:"skipped"`
	Missing      int              `json:"missing"`
	Failed       int              `json:"failed"`
	Cancelled    int              `json:"cancelled"`
	Results      []SyncFileResult `json:"results"`
}

type IngestionReport struct {
	SchemaVersion int         `json:"schema_version"`
	OK            bool        `json:"ok"`
	ExitCode      int         `json:"exit_code"`
	Summary       SyncSummary `json:"summary"`
	Pruned        int         `json:"pruned"`
	PruneRun      bool        `json:"prune_run"`
	Error         string      `json:"error,omitempty"`
}

// IngestWorkspaceReport is the shared CLI/MCP boundary. File failures do not
// discard successes; prune runs only after a successful reconciliation.
func (iw *IngestionWorker) IngestWorkspaceReport(ctx context.Context, prune bool) IngestionReport {
	summary, err := iw.SyncWorkspace(ctx)
	report := IngestionReport{SchemaVersion: 1, Summary: summary}
	if err == nil && prune {
		report.PruneRun = true
		report.Pruned, err = iw.PrunePending(ctx, time.Now())
	}
	report.OK = err == nil
	report.ExitCode = classifyOperationalError(err)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			report.ExitCode, report.Error = ExitPartialFailure, syncReasonDetail("cancelled")
		} else {
			report.Error = sanitizeOperationalError(err)
		}
	}
	return report
}

// tallyOutcomes recomputes the per-outcome counters from Results so the full
// workspace scan and single-file ingestion report identically.
func (s *SyncSummary) tallyOutcomes() {
	s.Created, s.Updated, s.Unchanged = 0, 0, 0
	s.Skipped, s.Missing, s.Failed, s.Cancelled = 0, 0, 0, 0
	for _, result := range s.Results {
		switch result.Outcome {
		case SyncCreated:
			s.Created++
		case SyncUpdated:
			s.Updated++
		case SyncUnchanged:
			s.Unchanged++
		case SyncSkipped:
			s.Skipped++
		case SyncMissing:
			s.Missing++
		case SyncCancelled:
			s.Cancelled++
		case SyncFailed:
			s.Failed++
		}
	}
	s.Ingested = s.Created + s.Updated
}

// IngestPathReport ingests a single already-published file (typically an
// envelope written under HIVE_DATA_DIR by `convert --ingest`) through the shared
// pipeline and returns the v1 report. Ownership, revision identity and
// idempotency are identical to a full scan restricted to that one path.
func (iw *IngestionWorker) IngestPathReport(ctx context.Context, absPath string) IngestionReport {
	summary := SyncSummary{ScanComplete: true, Total: 1, Results: []SyncFileResult{}}
	if !iw.Cfg.IsWriter() {
		report := IngestionReport{SchemaVersion: 1, Summary: summary, ExitCode: ExitAuthorization}
		report.Error = sanitizeOperationalError(&operationalError{ExitAuthorization, "single-file ingestion requires HIVE_ROLE=writer"})
		return report
	}
	err := iw.EnsureInfrastructure(ctx)
	if err == nil {
		result, syncErr := iw.syncFileResult(ctx, absPath)
		summary.Results = append(summary.Results, result)
		summary.tallyOutcomes()
		err = syncErr
	}
	report := IngestionReport{SchemaVersion: 1, Summary: summary}
	report.OK = err == nil
	report.ExitCode = classifyOperationalError(err)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			report.ExitCode, report.Error = ExitPartialFailure, syncReasonDetail("cancelled")
		} else {
			report.Error = sanitizeOperationalError(err)
		}
	}
	return report
}

// Tool failures use an MCP result with isError so the caller retains the report.
func ingestionMCPResponse(id json.RawMessage, report IngestionReport) map[string]any {
	encoded, _ := json.Marshal(report) // Only JSON-safe scalar/struct fields.
	return map[string]any{
		"jsonrpc": "2.0", "id": id,
		"result": map[string]any{
			"isError": !report.OK,
			"content": []map[string]any{{"type": "text", "text": string(encoded)}},
		},
	}
}

func (iw *IngestionWorker) syncReportPath(path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(iw.Cfg.DataDirectory, path)
	}
	if !pathWithin(iw.Cfg.DataDirectory, path) {
		return ""
	}
	rel, err := filepath.Rel(iw.Cfg.DataDirectory, path)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

type documentInputError struct {
	reason  string
	message string
}

func (e *documentInputError) Error() string { return e.message }

func syncReasonDetail(reason string) string {
	switch reason {
	case "foreign_writer":
		return "document is owned by another writer"
	case "ignored":
		return "document is excluded by ingestion policy"
	case "cancelled":
		return "ingestion was cancelled or its deadline expired"
	case "writer_required":
		return "ingestion requires a writer"
	case "missing_file":
		return "source disappeared; pending deletion was checked without removing vectors"
	case "pending_delete_failed":
		return "pending deletion could not be recorded"
	case "infrastructure_failed":
		return "ingestion infrastructure could not be verified"
	case "document_read_failed":
		return "document could not be read safely inside HIVE_DATA_DIR"
	case "scope_unavailable":
		return "approved scope could not be verified"
	case "invalid_metadata":
		return "document metadata is invalid"
	case "invalid_document":
		return "document could not be parsed within the configured limits"
	case "empty_document":
		return "document contains no indexable content"
	case "chunk_limit":
		return "document exceeds HIVE_MAX_CHUNKS_PER_FILE"
	case "file_size_limit":
		return "document exceeds HIVE_MAX_FILE_BYTES"
	case "unsupported_format":
		return "document format is not supported"
	case "invalid_encoding":
		return "document is binary or is not valid UTF-8"
	case "control_unavailable":
		return "document control state could not be verified"
	case "tombstoned":
		return "document was removed; explicit tombstone revocation is required"
	case "embedding_failed":
		return "document embedding failed"
	case "staging_failed":
		return "new document revision could not be staged"
	case "verification_failed":
		return "staged document revision could not be verified"
	case "commit_failed":
		return "document publication could not be confirmed; reconciliation is required"
	case "cleanup_pending":
		return "new revision is active; previous revision cleanup is pending"
	default:
		return "document synchronization failed"
	}
}
