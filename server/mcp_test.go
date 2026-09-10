package server

import (
	"context"
	"testing"
)

func TestReaderDoesNotExposeOrExecuteIngestion(t *testing.T) {
	worker := &IngestionWorker{Cfg: Config{Role: RoleReader}}
	if containsTool(worker.availableTools(), "ingest_workspace") {
		t.Fatal("reader exposed ingest_workspace")
	}
	if _, err := worker.SyncWorkspace(context.Background()); err == nil {
		t.Fatal("reader was allowed to synchronize the workspace")
	}
}

func TestWriterExposesIngestion(t *testing.T) {
	worker := &IngestionWorker{Cfg: Config{Role: RoleWriter}}
	if !containsTool(worker.availableTools(), "ingest_workspace") {
		t.Fatal("writer did not expose ingest_workspace")
	}
}

func containsTool(tools []map[string]interface{}, name string) bool {
	for _, tool := range tools {
		if tool["name"] == name {
			return true
		}
	}
	return false
}
