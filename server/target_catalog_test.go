package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qdrant/go-client/qdrant"
)

type staleFirstCatalogQdrant struct {
	*memoryQdrant
	collectionName string
	stale          *qdrant.RetrievedPoint
}

func (q *staleFirstCatalogQdrant) Scroll(ctx context.Context, in *qdrant.ScrollPoints) ([]*qdrant.RetrievedPoint, error) {
	rows, err := q.memoryQdrant.Scroll(ctx, in)
	if err != nil || in.CollectionName != q.collectionName || !matchesFilter(q.stale.Payload, in.Filter) {
		return rows, err
	}
	return append([]*qdrant.RetrievedPoint{q.stale}, rows...), nil
}

func TestHiveListTargetsBalancesApprovedTargetsWithoutCountingChunks(t *testing.T) {
	rules := `[{"action":"include","asset_type":"host","value":"api.example.com"},` +
		`{"action":"include","asset_type":"host","value":"alt.example.com"},` +
		`{"action":"include","asset_type":"host","value":"auth.example.com"},` +
		`{"action":"include","asset_type":"host","value":"empty.example.com"},` +
		`{"action":"include","asset_type":"host","value":"billing.example.com"},` +
		`{"action":"exclude","asset_type":"host","value":"billing.example.com"}]`
	worker, _, root, _ := setupContextWorker(t, rules)
	add := func(name, targetName, observed, kind, classification, body string) {
		t.Helper()
		opts, err := parseConvertArgs([]string{"-", "--format=txt", "--program=acme", "--platform=h1", "--target=" + targetName,
			"--observed-target=" + observed, "--classification=" + classification, "--document-type=" + kind, "--source=" + name})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := runConvert(context.Background(), opts, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "programs", "acme", name+".json")
		if err := writeConvertedDocument(path, encoded); err != nil {
			t.Fatal(err)
		}
		if report := worker.IngestPathReport(context.Background(), path); !report.OK {
			t.Fatalf("ingest %s: %+v", name, report)
		}
	}
	add("api-recon", "API Service", "api.example.com", "asset", "internal", strings.Repeat("api recon observations.\n", 300)+"alt.example.com\n")
	add("auth-recon", "Auth Portal", "auth.example.com", "asset", "internal", "auth recon")
	add("auth-notes", "Auth Portal", "auth.example.com", "note", "internal", "auth notes")
	add("empty-notes", "Empty Area", "empty.example.com", "note", "internal", "initial scope note")
	add("billing-recon", "Billing", "billing.example.com", "asset", "internal", "excluded billing recon")
	add("api-restricted", "API Service", "api.example.com", "evidence", "restricted", "restricted evidence")

	response, err := worker.HiveListTargets(context.Background(), HiveListTargetsArguments{ProgramID: "acme", IncludeUnconfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Targets) != 3 || response.Targets[0].TargetName != "API Service" || response.Targets[0].Band != "emerging" ||
		response.Targets[1].TargetName != "Auth Portal" || response.Targets[1].Band != "ready" ||
		response.Targets[2].TargetName != "Empty Area" || response.Targets[2].Band != "unmapped" {
		t.Fatalf("balanced catalog is wrong: %+v", response.Targets)
	}
	if response.Targets[0].Coverage.ReconDocuments != 1 || response.Targets[0].Coverage.EvidenceDocuments != 0 ||
		response.Targets[1].Coverage.ReconDocuments != 1 || response.Targets[1].Coverage.NoteDocuments != 1 {
		t.Fatalf("catalog counted chunks or crossed classification boundary: %+v", response.Targets)
	}
	if response.Targets[0].Scope.AuthorizedAssets != 2 || len(response.Targets[0].ObservedAssets) != 2 || response.Targets[0].Scope.ActionAllowed {
		t.Fatalf("observed host was not attached to project or project was marked actionable: %+v", response.Targets[0])
	}
	if len(response.Unconfirmed) != 1 || response.Unconfirmed[0].TargetName != "Billing" ||
		response.Unconfirmed[0].Scope.Status != "out_of_scope" || response.Unconfirmed[0].Scope.ActionAllowed {
		t.Fatalf("excluded target leaked into ranking: %+v", response.Unconfirmed)
	}
	if response.CandidatesEvaluated != 4 || response.Truncated {
		t.Fatalf("candidate accounting is wrong: %+v", response)
	}
	limit := 1
	limited, err := worker.HiveListTargets(context.Background(), HiveListTargetsArguments{ProgramID: "acme", Limit: &limit, Order: "needs_recon"})
	if err != nil || len(limited.Targets) != 1 || limited.Targets[0].TargetName != "Empty Area" || !limited.Truncated {
		t.Fatalf("needs_recon/limit failed: %+v, %v", limited, err)
	}
}

func TestHiveListTargetsHandlesMixedScopeLegacyGapAndReader(t *testing.T) {
	worker, _, root, _ := setupContextWorker(t,
		`[{"action":"include","asset_type":"host","value":"api.example.com"},`+
			`{"action":"exclude","asset_type":"host","value":"billing.example.com"}]`)
	if err := worker.SyncFileState(context.Background(), filepath.Join(root, "programs", "acme", "scope.json")); err != nil {
		t.Fatal(err)
	}
	opts, err := parseConvertArgs([]string{"-", "--format=txt", "--program=acme", "--platform=bugcrowd", "--target=Portal",
		"--classification=internal", "--document-type=asset", "--source=recon", "--observed-target=api.example.com", "--observed-target=billing.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := runConvert(context.Background(), opts, strings.NewReader("mixed observations"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "programs", "acme", "portal.json")
	if err := writeConvertedDocument(path, encoded); err != nil {
		t.Fatal(err)
	}
	if report := worker.IngestPathReport(context.Background(), path); !report.OK {
		t.Fatalf("ingest: %+v", report)
	}
	legacyPath := filepath.Join(root, "programs", "acme", "legacy.md")
	if err := os.WriteFile(legacyPath, []byte("---\nclassification: internal\n---\nlegacy note"), 0600); err != nil {
		t.Fatal(err)
	}
	if report := worker.IngestPathReport(context.Background(), legacyPath); !report.OK || !warningsContain(report.Summary.Results[0].Warnings, "target_registration_missing") {
		t.Fatalf("legacy warning missing: %+v", report)
	}
	worker.Cfg.Role = RoleReader
	worker.Cfg.DataDirectory = ""
	worker.Cfg.WatchDirectory = ""
	response, err := worker.HiveListTargets(context.Background(), HiveListTargetsArguments{ProgramID: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Targets) != 1 || response.Targets[0].TargetName != "Portal" || response.Targets[0].Scope.Status != "mixed" ||
		response.Targets[0].Scope.AuthorizedAssets != 1 || response.Targets[0].Scope.ExcludedAssets != 1 ||
		response.Targets[0].Coverage.ReconDocuments != 1 || response.UnregisteredFiles != 1 {
		t.Fatalf("reader catalog did not enforce mixed scope or report legacy gap: %+v", response)
	}
	if !containsTool(worker.availableTools(), "hive_list_targets") {
		t.Fatal("target catalog MCP tool is not advertised to readers")
	}
	for _, tool := range worker.availableTools() {
		if tool["name"] == "hive_list_targets" {
			if tool["inputSchema"].(map[string]interface{})["additionalProperties"] != false ||
				tool["outputSchema"].(map[string]interface{})["additionalProperties"] != false {
				t.Fatal("target catalog MCP schema is not closed")
			}
		}
	}
}

func TestHiveListTargetsRejectsInvalidArguments(t *testing.T) {
	bad := 51
	for _, args := range []HiveListTargetsArguments{{ProgramID: "../other"}, {ProgramID: "acme", Limit: &bad}, {ProgramID: "acme", Order: "random"}} {
		if _, _, err := validateHiveListTargets(args); err == nil {
			t.Fatalf("accepted invalid catalog request: %+v", args)
		}
	}
}

func TestProgramWideScopeRegistration(t *testing.T) {
	manifest := `{"schema_version":1,"program_id":"acme","platform":"h1","target_name":"@program","classification":"internal",` +
		`"source":"policy","collected_at":"2026-09-23T00:00:00Z","rules":[{"action":"include","asset_type":"host","value":"api.example.com"}]}`
	if _, err := parseScopeManifest([]byte(manifest), "acme"); err != nil {
		t.Fatal(err)
	}
	meta, err := extractReconMetadata("programs/acme/scope.json", "acme", []byte(manifest))
	if err != nil || meta.Platform != "h1" || meta.TargetName != "@program" || meta.Classification != "internal" {
		t.Fatalf("program-wide registration lost: %+v, %v", meta, err)
	}
}

func TestHiveListTargetsDropsOldRevisionsAndRemovedDocuments(t *testing.T) {
	worker, _, root, _ := setupContextWorker(t, `[{"action":"include","asset_type":"host","value":"api.example.com"}]`)
	path := filepath.Join(root, "programs", "acme", "project.json")
	makeEnvelope := func(name string) []byte {
		t.Helper()
		opts, err := parseConvertArgs([]string{"-", "--format=txt", "--program=acme", "--platform=h1", "--target=" + name,
			"--observed-target=api.example.com", "--classification=internal", "--document-type=asset", "--source=recon"})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := runConvert(context.Background(), opts, strings.NewReader("api observations"))
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	if err := writeConvertedDocument(path, makeEnvelope("Old Project")); err != nil {
		t.Fatal(err)
	}
	if report := worker.IngestPathReport(context.Background(), path); !report.OK {
		t.Fatalf("first ingest: %+v", report)
	}
	if err := os.WriteFile(path, makeEnvelope("New Project"), 0600); err != nil {
		t.Fatal(err)
	}
	if report := worker.IngestPathReport(context.Background(), path); !report.OK {
		t.Fatalf("second ingest: %+v", report)
	}
	response, err := worker.HiveListTargets(context.Background(), HiveListTargetsArguments{ProgramID: "acme"})
	if err != nil || len(response.Targets) != 1 || response.Targets[0].TargetName != "New Project" {
		t.Fatalf("stale target survived revision: %+v, %v", response, err)
	}
	if err := worker.RemoveDocument(context.Background(), path, "operator_requested"); err != nil {
		t.Fatal(err)
	}
	response, err = worker.HiveListTargets(context.Background(), HiveListTargetsArguments{ProgramID: "acme"})
	if err != nil || len(response.Targets) != 0 {
		t.Fatalf("removed document survived in catalog: %+v, %v", response, err)
	}
}

func TestHiveListTargetsSkipsStaleRowBeforeActiveRevision(t *testing.T) {
	worker, q, root, _ := setupContextWorker(t, `[{"action":"include","asset_type":"host","value":"api.example.com"}]`)
	opts, err := parseConvertArgs([]string{"-", "--format=txt", "--program=acme", "--platform=h1", "--target=API Service",
		"--observed-target=api.example.com", "--classification=internal", "--document-type=asset", "--source=recon"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := runConvert(context.Background(), opts, strings.NewReader("api.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "programs", "acme", "api.json")
	if err := writeConvertedDocument(path, encoded); err != nil {
		t.Fatal(err)
	}
	if report := worker.IngestPathReport(context.Background(), path); !report.OK {
		t.Fatalf("ingest: %+v", report)
	}
	var stale *qdrant.RetrievedPoint
	for _, point := range q.points[worker.Cfg.CollectionName] {
		if payloadString(point.Payload, "path", "") != "programs/acme/api.json" || payloadInt(point.Payload, "chunk_ordinal") != 0 {
			continue
		}
		payload := make(map[string]*qdrant.Value, len(point.Payload))
		for key, value := range point.Payload {
			payload[key] = value
		}
		payload["document_revision"] = qdrant.NewValueString("obsolete-revision")
		stale = &qdrant.RetrievedPoint{Id: qdrant.NewIDUUID(deterministicUUID("stale", path)), Payload: payload}
		break
	}
	if stale == nil {
		t.Fatal("active catalog row not found")
	}
	worker.QdrantClient = &staleFirstCatalogQdrant{memoryQdrant: q, collectionName: worker.Cfg.CollectionName, stale: stale}
	response, err := worker.HiveListTargets(context.Background(), HiveListTargetsArguments{ProgramID: "acme"})
	if err != nil || len(response.Targets) != 1 || response.Targets[0].TargetName != "API Service" {
		t.Fatalf("stale row hid active revision: %+v, %v", response, err)
	}
}

func TestHiveListTargetsRanksAcrossProgramsWithoutProgramID(t *testing.T) {
	worker, _, root, _ := setupContextWorker(t, `[{"action":"include","asset_type":"wildcard_domain","value":"*.acme.example.com"}]`)
	for _, tool := range worker.availableTools() {
		if tool["name"] == "hive_list_targets" {
			input := tool["inputSchema"].(map[string]interface{})
			if required, ok := input["required"]; ok && len(required.([]string)) > 0 {
				t.Fatalf("MCP still requires a program_id for global discovery: %v", required)
			}
		}
	}
	betaDir := filepath.Join(root, "programs", "beta")
	if err := os.MkdirAll(betaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	betaScope := `{"schema_version":1,"program_id":"beta","source":"policy","collected_at":"2026-09-10T00:00:00Z",` +
		`"rules":[{"action":"include","asset_type":"wildcard_domain","value":"*.beta.example.com"}]}`
	if err := os.WriteFile(filepath.Join(betaDir, "scope.json"), []byte(betaScope), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := worker.ApproveScope(context.Background(), "beta"); err != nil {
		t.Fatal(err)
	}
	add := func(program string, index int, kind string) {
		t.Helper()
		host := fmt.Sprintf("host%d.%s.example.com", index, program)
		name := fmt.Sprintf("Project %02d", index)
		opts, err := parseConvertArgs([]string{"-", "--format=txt", "--program=" + program, "--platform=h1", "--target=" + name,
			"--observed-target=" + host, "--classification=internal", "--document-type=" + kind, "--source=" + name})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := runConvert(context.Background(), opts, strings.NewReader(host))
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "programs", program, fmt.Sprintf("project-%02d-%s.json", index, kind))
		if err := writeConvertedDocument(path, encoded); err != nil {
			t.Fatal(err)
		}
		if report := worker.IngestPathReport(context.Background(), path); !report.OK {
			t.Fatalf("ingest %s: %+v", path, report)
		}
	}
	for _, program := range []string{"acme", "beta"} {
		for index := range 6 {
			add(program, index, "asset")
		}
	}
	add("beta", 0, "note")

	response, err := worker.HiveListTargets(context.Background(), HiveListTargetsArguments{})
	if err != nil || len(response.Targets) != 10 || !response.Truncated || response.CandidatesEvaluated != 12 {
		t.Fatalf("global top 10 failed: %+v, %v", response, err)
	}
	seen := map[string]bool{}
	for _, target := range response.Targets {
		seen[target.ProgramID] = true
		if target.Scope.AuthorizedAssets != 1 || target.Scope.ScopeRevision == "" || target.Rank < 1 || target.Rank > 10 {
			t.Fatalf("global target lacks program provenance or scope: %+v", target)
		}
	}
	if !seen["acme"] || !seen["beta"] {
		t.Fatalf("global top 10 did not combine programs: %+v", response.Targets)
	}
	limit := 1
	documented, err := worker.HiveListTargets(context.Background(), HiveListTargetsArguments{Limit: &limit, Order: "most_documented"})
	if err != nil || len(documented.Targets) != 1 || documented.Targets[0].ProgramID != "beta" ||
		documented.Targets[0].TargetName != "Project 00" || documented.Targets[0].Coverage.NoteDocuments != 1 {
		t.Fatalf("global ranking did not compare program coverage: %+v, %v", documented, err)
	}
}
