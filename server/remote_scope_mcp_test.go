package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteMCPScopeApprovalWorkflow(t *testing.T) {
	srv, key, kid := testHTTPServer(t)
	setTestHome(t, t.TempDir())
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()
	client := NewRemoteClient(httpSrv.URL)
	programID := "remote-scope-mcp"
	manifest := `{"schema_version":1,"program_id":"remote-scope-mcp","platform":"h1","target_name":"@program","classification":"internal","source":"synthetic portal","collected_at":"2026-10-07T12:00:00Z","rules":[{"action":"include","asset_type":"host","value":"api.example.test"}]}`

	reader := memberToken(t, srv, key, kid, []string{permissionMindRead}, nil)
	if err := storeToken(reader); err != nil {
		t.Fatal(err)
	}
	if client.CanApproveScope() {
		t.Fatal("reader exposed scope approval tools")
	}
	if _, err := client.PreviewScopeApproval(context.Background(), programID); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("reader preview error = %v", err)
	}

	admin := memberToken(t, srv, key, kid, []string{permissionMindRead, permissionScopeAdmin}, nil)
	if err := storeToken(admin); err != nil {
		t.Fatal(err)
	}
	listed := remoteMCPReply(t, client, "tools/list", nil)
	var capabilities struct {
		Tools []map[string]interface{} `json:"tools"`
	}
	if err := json.Unmarshal(listed["result"], &capabilities); err != nil {
		t.Fatal(err)
	}
	if !containsTool(capabilities.Tools, "hive_preview_scope_approval") || !containsTool(capabilities.Tools, "hive_approve_scope") {
		t.Fatal("scope approval tools were not advertised to an authorized member")
	}

	args := HiveIngestDocumentArguments{ProgramID: programID, Classification: "internal", DocumentType: "scope", SourceFormat: "json", Content: manifest}
	if report := client.IngestDocument(context.Background(), args); !report.OK {
		t.Fatalf("scope publication: %+v", report)
	}
	if err := storeToken(reader); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ApproveScopeRevision(context.Background(), programID, sha256Hex([]byte(manifest))); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("reader approval error = %v", err)
	}
	if err := storeToken(admin); err != nil {
		t.Fatal(err)
	}
	previewCall := CallToolParams{Name: "hive_preview_scope_approval", Arguments: json.RawMessage(`{"program_id":"remote-scope-mcp"}`)}
	previewReply := remoteMCPReply(t, client, "tools/call", previewCall)
	var previewResult struct {
		StructuredContent ScopeApprovalSummary `json:"structuredContent"`
		IsError           bool                 `json:"isError"`
	}
	if err := json.Unmarshal(previewReply["result"], &previewResult); err != nil || previewResult.IsError {
		t.Fatalf("scope preview: %s; %v", previewReply["result"], err)
	}
	if previewResult.StructuredContent.SHA256 != sha256Hex([]byte(manifest)) || previewResult.StructuredContent.Includes != 1 {
		t.Fatalf("unexpected scope preview: %+v", previewResult.StructuredContent)
	}

	args.Content = strings.Replace(manifest, "api.example.test", "new.example.test", 1)
	if report := client.IngestDocument(context.Background(), args); !report.OK {
		t.Fatalf("changed scope publication: %+v", report)
	}
	stale, _ := json.Marshal(map[string]string{"program_id": programID, "sha256": previewResult.StructuredContent.SHA256})
	staleReply := remoteMCPReply(t, client, "tools/call", CallToolParams{Name: "hive_approve_scope", Arguments: stale})
	var failed struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(staleReply["result"], &failed); err != nil || !failed.IsError || len(failed.Content) != 1 || !strings.Contains(failed.Content[0].Text, "conflict") {
		t.Fatalf("stale approval did not fail safely: %s; %v", staleReply["result"], err)
	}
	revision, _, err := srv.worker.resolveActiveScope(context.Background(), programID)
	if err != nil || revision != "unapproved" {
		t.Fatalf("stale approval activated scope: revision=%s err=%v", revision, err)
	}

	freshReply := remoteMCPReply(t, client, "tools/call", previewCall)
	if err := json.Unmarshal(freshReply["result"], &previewResult); err != nil {
		t.Fatal(err)
	}
	freshHash := previewResult.StructuredContent.SHA256
	if freshHash != sha256Hex([]byte(args.Content)) {
		t.Fatalf("new preview hash = %s", freshHash)
	}
	confirmed, _ := json.Marshal(map[string]string{"program_id": programID, "sha256": freshHash})
	approveReply := remoteMCPReply(t, client, "tools/call", CallToolParams{Name: "hive_approve_scope", Arguments: confirmed})
	var approved struct {
		StructuredContent ScopeApprovalResult `json:"structuredContent"`
		IsError           bool                `json:"isError"`
	}
	if err := json.Unmarshal(approveReply["result"], &approved); err != nil || approved.IsError || approved.StructuredContent.Status != "approved" || approved.StructuredContent.ScopeRevision != freshHash {
		t.Fatalf("MCP approval: %s; %v", approveReply["result"], err)
	}
	revision, _, err = srv.worker.resolveActiveScope(context.Background(), programID)
	if err != nil || revision != freshHash {
		t.Fatalf("scope was not activated: revision=%s err=%v", revision, err)
	}
}
