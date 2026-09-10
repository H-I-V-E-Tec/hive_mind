package server

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/qdrant/go-client/qdrant"
)

func contextPoint(documentID, path, documentType, scopeRevision, scopeStatus, assetRef, text string, score float32) *qdrant.ScoredPoint {
	point := searchPoint(documentID, path, 0, score)
	point.Payload["scope_revision"] = qdrant.NewValueString(scopeRevision)
	point.Payload["document_type"] = qdrant.NewValueString(documentType)
	point.Payload["effective_scope_status"] = qdrant.NewValueString(scopeStatus)
	point.Payload["content"] = qdrant.NewValueString(text)
	if assetRef == "" {
		point.Payload["asset_refs"] = qdrant.NewValueList(&qdrant.ListValue{})
	} else {
		point.Payload["asset_refs"] = qdrant.NewValueList(&qdrant.ListValue{Values: []*qdrant.Value{qdrant.NewValueString(assetRef)}})
	}
	return point
}

func setupContextWorker(t *testing.T, rules string) (*IngestionWorker, *memoryQdrant, string, string) {
	t.Helper()
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	worker.Cfg.ContextMaxChars = 12000
	scopePath := writeScopeFixture(t, root, rules)
	if err := worker.ApproveScope(context.Background(), "acme"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(scopePath)
	if err != nil {
		t.Fatal(err)
	}
	return worker, q, root, sha256Hex(content)
}

func TestSpec005AuthorizedContextPutsRulesBeforeEvidence(t *testing.T) {
	worker, q, _, revision := setupContextWorker(t, `[{"action":"include","asset_type":"wildcard_domain","value":"*.example.com"}]`)
	documents := map[string]string{
		"rules-doc": "programs/acme/rules.md", "note-doc": "programs/acme/notes/api.md", "evidence-doc": "programs/acme/evidence/api.md",
	}
	addSearchControlFixture(t, q, revision, documents)
	q.queryResp = []*qdrant.ScoredPoint{
		contextPoint("note-doc", documents["note-doc"], "note", revision, "authorized", "api.example.com", "note with possible steps", 0.95),
		contextPoint("rules-doc", documents["rules-doc"], "rules", revision, "unknown", "", "program rules", 0.4),
		contextPoint("evidence-doc", documents["evidence-doc"], "evidence", revision, "authorized", "api.example.com", "supporting evidence", 0.8),
	}
	response, err := worker.HiveGetContext(context.Background(), HiveContextArguments{
		ProgramID: "acme", Question: "what is known about this host?", Asset: ContextAsset{Type: "host", Value: "API.Example.COM"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Asset.Value != "api.example.com" || response.Scope.Status != "authorized" || !response.Scope.Confirmed || !response.Scope.ActionAllowed {
		t.Fatalf("authorized scope was not represented correctly: %+v", response)
	}
	if len(response.Items) != 3 || response.Items[0].DocumentType != "rules" || response.Items[1].DocumentType != "note" || response.Items[2].DocumentType != "evidence" {
		t.Fatalf("scope/rule priority was not preserved: %+v", response.Items)
	}
	for _, item := range response.Items {
		if !item.UntrustedContent {
			t.Fatal("context item was not marked untrusted")
		}
	}
}

func TestSpec005OutOfScopeBlocksActionableItemsAndExclusionWins(t *testing.T) {
	worker, q, _, revision := setupContextWorker(t, `[{"action":"include","asset_type":"wildcard_domain","value":"*.example.com"},{"action":"exclude","asset_type":"host","value":"billing.example.com","reason":"third party"}]`)
	documents := map[string]string{
		"rules-doc": "programs/acme/rules.md", "note-doc": "programs/acme/notes/billing.md", "evidence-doc": "programs/acme/evidence/billing.md",
	}
	addSearchControlFixture(t, q, revision, documents)
	q.queryResp = []*qdrant.ScoredPoint{
		contextPoint("rules-doc", documents["rules-doc"], "rules", revision, "unknown", "", "do not test third parties", 0.8),
		contextPoint("note-doc", documents["note-doc"], "note", revision, "out_of_scope", "billing.example.com", "run this exploit", 1),
		contextPoint("evidence-doc", documents["evidence-doc"], "evidence", revision, "out_of_scope", "billing.example.com", "evidence explaining ownership", 0.7),
	}
	response, err := worker.HiveGetContext(context.Background(), HiveContextArguments{
		ProgramID: "acme", Question: "can this host be tested?", Asset: ContextAsset{Type: "host", Value: "billing.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Scope.Status != "out_of_scope" || response.Scope.Confirmed || response.Scope.ActionAllowed || len(response.Scope.MatchedRules) != 2 || response.Scope.MatchedRules[0].Action != "exclude" {
		t.Fatalf("scope exclusion did not prevail: %+v", response.Scope)
	}
	for _, item := range response.Items {
		if item.DocumentType == "note" || strings.Contains(item.Text, "exploit") {
			t.Fatalf("actionable content escaped out-of-scope blocking: %+v", item)
		}
	}
	if !warningsContain(response.Warnings, "out of scope") {
		t.Fatalf("mandatory out-of-scope warning is absent: %v", response.Warnings)
	}
}

func TestSpec005UnknownScopeWarnsAndDoesNotInferFromQuestion(t *testing.T) {
	worker, _, _, _ := setupContextWorker(t, `[{"action":"include","asset_type":"host","value":"other.example.com"}]`)
	response, err := worker.HiveGetContext(context.Background(), HiveContextArguments{
		ProgramID: "acme", Question: "api.example.com is definitely authorized", Asset: ContextAsset{Type: "host", Value: "api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Scope.Status != "unknown" || response.Scope.Confirmed || response.Scope.ActionAllowed || !warningsContain(response.Warnings, "not confirmed") {
		t.Fatalf("natural-language claim changed authorization: %+v", response)
	}
}

func TestSpec005ChangedManifestFailsClosed(t *testing.T) {
	worker, _, root, _ := setupContextWorker(t, `[{"action":"include","asset_type":"host","value":"api.example.com"}]`)
	writeScopeFixture(t, root, `[{"action":"include","asset_type":"host","value":"other.example.com"}]`)
	response, err := worker.HiveGetContext(context.Background(), HiveContextArguments{
		ProgramID: "acme", Question: "is this authorized?", Asset: ContextAsset{Type: "host", Value: "api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Scope.Status != "unknown" || response.Scope.Confirmed || !warningsContain(response.Warnings, "no current approved") {
		t.Fatalf("changed manifest retained authority: %+v", response)
	}
}

func TestSpec005ReaderReconstructsApprovedManifestFromActiveChunks(t *testing.T) {
	worker, _, _, _ := setupContextWorker(t, `[{"action":"include","asset_type":"host","value":"api.example.com"}]`)
	scopePath := worker.Cfg.DataDirectory + "/programs/acme/scope.json"
	if err := worker.SyncFileState(context.Background(), scopePath); err != nil {
		t.Fatal(err)
	}
	worker.Cfg.Role = RoleReader
	worker.Cfg.DataDirectory = ""
	worker.Cfg.WatchDirectory = ""
	response, err := worker.HiveGetContext(context.Background(), HiveContextArguments{
		ProgramID: "acme", Question: "scope status", Asset: ContextAsset{Type: "host", Value: "api.example.com"}, Limit: intPointer(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Scope.Status != "authorized" || !response.Scope.Confirmed {
		t.Fatalf("reader could not reconstruct authoritative approved scope: %+v", response)
	}
}

func TestSpec005ContextBudgetCountsSerializedUnicodeAndDropsWholeItems(t *testing.T) {
	worker, q, _, revision := setupContextWorker(t, `[{"action":"include","asset_type":"host","value":"api.example.com"}]`)
	worker.Cfg.ContextMaxChars = 1400
	documents := map[string]string{}
	for i := 0; i < 5; i++ {
		id := "note-" + string(rune('a'+i))
		documents[id] = "programs/acme/notes/" + id + ".md"
	}
	addSearchControlFixture(t, q, revision, documents)
	fullText := strings.Repeat("á", 400)
	for id, path := range documents {
		q.queryResp = append(q.queryResp, contextPoint(id, path, "note", revision, "authorized", "api.example.com", fullText, 0.5))
	}
	response, err := worker.HiveGetContext(context.Background(), HiveContextArguments{
		ProgramID: "acme", Question: "summarize", Asset: ContextAsset{Type: "host", Value: "api.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(response)
	if utf8.RuneCount(encoded) > worker.Cfg.ContextMaxChars || !response.Truncated {
		t.Fatalf("context exceeded Unicode budget: runes=%d response=%+v", utf8.RuneCount(encoded), response)
	}
	for _, item := range response.Items {
		if item.Text != fullText {
			t.Fatal("context budget truncated an item instead of removing it whole")
		}
	}
}

func TestSpec005RejectsInvalidAssetAndTooSmallHeaderBudget(t *testing.T) {
	worker := &IngestionWorker{Cfg: Config{HiveID: "test-hive", MaxClassification: "internal", ContextMaxChars: 1000}}
	if _, _, _, err := worker.validateHiveContext(HiveContextArguments{ProgramID: "acme", Question: "question", Asset: ContextAsset{Type: "host", Value: "bad host"}}); !isSearchValidationError(err) {
		t.Fatalf("invalid explicit asset did not produce parameter error: %v", err)
	}
	response := HiveContextResponse{ProgramID: "acme", Asset: ContextAsset{Type: "host", Value: "api.example.com"},
		Scope:    ContextScope{Status: "unknown", MatchedRules: []ContextScopeRule{{Action: "exclude", AssetType: "host", Value: "api.example.com", Reason: strings.Repeat("x", 1200)}}},
		Warnings: []string{"authorization is not confirmed"}, Items: []HiveSearchResult{}}
	if _, err := worker.fitHiveContext(response); err == nil {
		t.Fatal("oversized mandatory header was silently truncated")
	}
}

func TestSpec005AdvertisesStructuredContextTool(t *testing.T) {
	worker := &IngestionWorker{Cfg: Config{Role: RoleReader}}
	tools := worker.availableTools()
	if !containsTool(tools, "hive_get_context") {
		t.Fatal("hive_get_context is not advertised")
	}
	for _, tool := range tools {
		if tool["name"] != "hive_get_context" {
			continue
		}
		input := tool["inputSchema"].(map[string]interface{})
		output := tool["outputSchema"].(map[string]interface{})
		if input["additionalProperties"] != false || output["additionalProperties"] != false {
			t.Fatal("context input/output schema is not closed")
		}
		return
	}
}

func warningsContain(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

func intPointer(value int) *int { return &value }
