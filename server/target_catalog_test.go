package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
