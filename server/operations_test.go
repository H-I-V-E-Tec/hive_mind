package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSpec007ParsesSearchFiltersWithoutMixingThemIntoQuery(t *testing.T) {
	args, err := parseSearchCLI([]string{
		"acme", "oauth", "hosts", "--document-type=asset", "--document-type=endpoint",
		"--tag=recon", "--classification=internal", "--scope-status=authorized", "--limit=5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if args.ProgramID != "acme" || args.Query != "oauth hosts" || len(args.DocumentTypes) != 2 ||
		len(args.Tags) != 1 || args.Classification != "internal" || args.EffectiveScopeStatus != "authorized" ||
		args.Limit == nil || *args.Limit != 5 {
		t.Fatalf("unexpected parsed search: %+v", args)
	}
	for _, invalid := range [][]string{{"acme"}, {"acme", "query", "--limit=nope"}, {"acme", "query", "--hive-id=other"}} {
		if _, err := parseSearchCLI(invalid); err == nil {
			t.Fatalf("invalid search arguments accepted: %v", invalid)
		}
	}
}

func TestSpec007CLISeparatesConfigurationAndRejectsUnknownCommands(t *testing.T) {
	args, err := splitCLIArgs([]string{"hive", "--config", "/private/hive.toml", "scope", "approve", "acme", "--yes", "--role=writer"})
	if err != nil || strings.Join(args, " ") != "hive scope approve acme --yes" {
		t.Fatalf("config leaked into positional arguments: %v %v", args, err)
	}
	for _, raw := range [][]string{{"hive", "typo"}, {"hive", "status", "extra"}, {"hive", "ingest", "--yes"}, {"hive", "--config"}} {
		if _, err := splitCLIArgs(raw); err == nil {
			t.Fatalf("invalid CLI accepted: %v", raw)
		}
	}
	for _, limit := range []string{"0", "-1", "18446744073709551617"} {
		if _, err := parsePositiveInt(limit); err == nil {
			t.Fatalf("invalid/overflowed limit accepted: %s", limit)
		}
	}
}

func TestSpec007StatusIsSanitizedAndReportsSynchronization(t *testing.T) {
	q := newMemoryQdrant()
	worker, _ := specWorker(t, q)
	worker.Cfg.QdrantAPIKey = newSecret("status-sentinel-secret")
	if err := worker.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := "2026-09-11T12:00:00Z"
	if err := worker.upsertControl(context.Background(), "document_head", "doc-1", map[string]any{
		"document_id": "doc-1", "program_id": "acme", "state": "pending_delete", "updated_at": now,
	}); err != nil {
		t.Fatal(err)
	}
	report, err := worker.OperationalStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Credential != "[REDACTED]" || report.PendingDocuments != 1 || report.LastSynchronization != now || report.EmbeddingDimension != 3 {
		t.Fatalf("unexpected operational status: %+v", report)
	}
	if strings.Contains(report.Credential, "sentinel") {
		t.Fatal("status leaked the credential")
	}
}

func TestSpec007ValidateReportsRolePermissionsAndFingerprint(t *testing.T) {
	q := newMemoryQdrant()
	worker, _ := specWorker(t, q)
	worker.Cfg.QdrantAPIKey = newSecret("synthetic-writer-token")
	worker.Cfg.QdrantHost = "127.0.0.1"
	if err := worker.EnsureInfrastructure(context.Background()); err != nil {
		t.Fatal(err)
	}
	worker.infrastructureReady = false
	report := worker.ValidateOperational(context.Background())
	if !report.OK || report.ExitCode != ExitOK {
		t.Fatalf("healthy writer validation failed: %+v", report)
	}
	want := map[string]bool{
		"configuration": false, "writer_data_directory": false, "qdrant_connectivity_and_tls": false,
		"infrastructure_and_fingerprint": false, "credential_capabilities": false,
	}
	for _, check := range report.Checks {
		if _, ok := want[check.Name]; ok && check.Status == "passed" {
			want[check.Name] = true
		}
	}
	for name, passed := range want {
		if !passed {
			t.Errorf("validation check %q did not pass: %+v", name, report.Checks)
		}
	}
}

func TestSpec007OperationalExitCodesAndErrorsAreStable(t *testing.T) {
	tests := []struct {
		err  error
		code int
		text string
	}{
		{status.Error(codes.Unavailable, "private endpoint"), ExitConnectivity, "required service is unavailable"},
		{assertError("configuration is invalid"), ExitConfiguration, "configuration is invalid"},
		{status.Error(codes.PermissionDenied, "token detail"), ExitAuthorization, "Qdrant authentication or authorization failed"},
		{os.ErrInvalid, ExitConnectivity, "required service is unavailable"},
		{assertError("x509: certificate expired"), ExitTLS, "Qdrant TLS validation failed"},
		{assertError("incompatible collection schema"), ExitCompatibility, "embedding or collection schema is incompatible"},
		{assertError("cleanup pending after partial commit"), ExitPartialFailure, "operation is incomplete and recoverable"},
	}
	for _, test := range tests {
		if got := classifyOperationalError(test.err); got != test.code {
			t.Errorf("classify %q: got %d want %d", test.err, got, test.code)
		}
		if got := sanitizeOperationalError(test.err); got != test.text {
			t.Errorf("sanitize %q: got %q want %q", test.err, got, test.text)
		}
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }

func TestSpec007ScopeApprovalPreviewContainsOnlySummaryAndDoesNotWrite(t *testing.T) {
	q := newMemoryQdrant()
	worker, root := specWorker(t, q)
	manifest := `{"schema_version":1,"program_id":"acme","source":"operator portal","collected_at":"2026-09-11T12:00:00Z","rules":[{"action":"include","asset_type":"host","value":"api.example.com"},{"action":"exclude","asset_type":"host","value":"admin.example.com"}]}`
	path := filepath.Join(root, "programs", "acme", "scope.json")
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	summary, err := worker.ScopeApprovalPreview("acme")
	if err != nil {
		t.Fatal(err)
	}
	if summary.ProgramID != "acme" || len(summary.SHA256) != 64 || summary.Rules != 2 || summary.Includes != 1 || summary.Excludes != 1 {
		t.Fatalf("unexpected preview: %+v", summary)
	}
	if len(q.events) != 0 || len(q.points) != 0 {
		t.Fatalf("preview changed Qdrant: events=%v points=%v", q.events, q.points)
	}
}
