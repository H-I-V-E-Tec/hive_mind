package server

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMCPDoesNotExposeWorkspaceIngestion(t *testing.T) {
	if containsTool(mcpAvailableTools(), "ingest_workspace") {
		t.Fatal("MCP exposed ingest_workspace")
	}
	worker := &IngestionWorker{Cfg: Config{Role: RoleReader}}
	if _, err := worker.SyncWorkspace(context.Background()); err == nil {
		t.Fatal("reader was allowed to synchronize the workspace")
	}
}

func TestMCPRejectsWorkspaceIngestionEvenForWriter(t *testing.T) {
	worker, _ := specWorker(t, newMemoryQdrant())
	reply := remoteMCPReply(t, &workerBackend{worker: worker}, "tools/call", CallToolParams{
		Name: "ingest_workspace", Arguments: json.RawMessage(`{}`),
	})
	var failure struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(reply["error"], &failure); err != nil || failure.Code != -32601 {
		t.Fatalf("workspace scan was not rejected: %v (%v)", reply, err)
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
