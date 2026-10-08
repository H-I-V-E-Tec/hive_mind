package server

import (
	"context"
	"errors"
)

type SyncStatusSnapshot struct {
	Status       string `json:"status"`
	PendingFiles int    `json:"pending_files"`
	ActiveSyncs  int    `json:"active_syncs"`
	TotalSynced  int    `json:"total_synced"`
}

type HiveBackend interface {
	HiveSearch(ctx context.Context, args HiveSearchArguments) (HiveSearchResponse, error)
	HiveGetContext(ctx context.Context, args HiveContextArguments) (HiveContextResponse, error)
	HiveListTargets(ctx context.Context, args HiveListTargetsArguments) (HiveListTargetsResponse, error)
	IngestDocument(ctx context.Context, args HiveIngestDocumentArguments) IngestionReport
	CanIngestDocument() bool
	CanApproveScope() bool
	PreviewScopeApproval(ctx context.Context, programID string) (ScopeApprovalSummary, error)
	ApproveScopeRevision(ctx context.Context, programID, sha256 string) (ScopeApprovalResult, error)
	SyncStatus() (SyncStatusSnapshot, error)
	Close()
}

// workerBackend adapts *IngestionWorker to the HiveBackend interface.
type workerBackend struct {
	worker *IngestionWorker
}

func (w *workerBackend) HiveSearch(ctx context.Context, args HiveSearchArguments) (HiveSearchResponse, error) {
	return w.worker.HiveSearch(ctx, args)
}

func (w *workerBackend) HiveGetContext(ctx context.Context, args HiveContextArguments) (HiveContextResponse, error) {
	return w.worker.HiveGetContext(ctx, args)
}

func (w *workerBackend) HiveListTargets(ctx context.Context, args HiveListTargetsArguments) (HiveListTargetsResponse, error) {
	return w.worker.HiveListTargets(ctx, args)
}

func (w *workerBackend) SyncStatus() (SyncStatusSnapshot, error) {
	w.worker.Mu.Lock()
	defer w.worker.Mu.Unlock()
	status := "idle"
	if len(w.worker.PendingFiles) > 0 || w.worker.ActiveSyncs > 0 {
		status = "syncing"
	}
	return SyncStatusSnapshot{
		Status:       status,
		PendingFiles: len(w.worker.PendingFiles),
		ActiveSyncs:  w.worker.ActiveSyncs,
		TotalSynced:  w.worker.TotalSynced,
	}, nil
}

func (w *workerBackend) CanIngestDocument() bool { return false }

func (w *workerBackend) CanApproveScope() bool { return false }

func (w *workerBackend) PreviewScopeApproval(context.Context, string) (ScopeApprovalSummary, error) {
	return ScopeApprovalSummary{}, errors.New("scope approval requires a remote member session")
}

func (w *workerBackend) ApproveScopeRevision(context.Context, string, string) (ScopeApprovalResult, error) {
	return ScopeApprovalResult{}, errors.New("scope approval requires a remote member session")
}

func (w *workerBackend) IngestDocument(context.Context, HiveIngestDocumentArguments) IngestionReport {
	return failedDocumentReport(ExitAuthorization, "content ingestion requires a remote member session")
}

func (w *workerBackend) Close() {
	w.worker.Close()
}
