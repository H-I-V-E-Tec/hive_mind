package server

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ExitOK             = 0
	ExitUsage          = 2
	ExitConfiguration  = 10
	ExitConnectivity   = 11
	ExitAuthorization  = 12
	ExitTLS            = 13
	ExitCompatibility  = 14
	ExitPartialFailure = 15
)

type OperationalStatus struct {
	HiveID              string   `json:"hive_id"`
	DeviceID            string   `json:"device_id"`
	Role                string   `json:"role"`
	DataCollection      string   `json:"data_collection"`
	ControlCollection   string   `json:"control_collection"`
	EmbeddingModel      string   `json:"embedding_model"`
	EmbeddingDimension  int64    `json:"embedding_dimension"`
	MaxClassification   string   `json:"max_classification"`
	QdrantTLS           bool     `json:"qdrant_tls"`
	Credential          string   `json:"credential"`
	LastSynchronization any      `json:"last_synchronization"`
	PendingDocuments    int      `json:"pending_documents"`
	Warnings            []string `json:"warnings"`
}

type ValidationCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type ValidationReport struct {
	OK       bool              `json:"ok"`
	ExitCode int               `json:"exit_code"`
	Checks   []ValidationCheck `json:"checks"`
}

func (iw *IngestionWorker) OperationalStatus(ctx context.Context) (OperationalStatus, error) {
	report := OperationalStatus{
		HiveID: iw.Cfg.HiveID, DeviceID: iw.Cfg.DeviceID, Role: iw.Cfg.Role,
		DataCollection: iw.Cfg.CollectionName, ControlCollection: iw.Cfg.ControlCollection,
		EmbeddingModel: iw.Cfg.EmbeddingModel, MaxClassification: iw.Cfg.MaxClassification,
		QdrantTLS: iw.Cfg.QdrantUseTLS, Credential: "[REDACTED]", Warnings: []string{},
	}
	manifests, err := iw.controlRows(ctx, "collection_manifest", iw.Cfg.HiveID)
	if err != nil || len(manifests) != 1 {
		return OperationalStatus{}, errors.New("collection manifest is unavailable")
	}
	report.EmbeddingDimension = payloadInt(manifests[0].Payload, "vector_dimension")
	filter := &qdrant.Filter{Must: []*qdrant.Condition{
		qdrant.NewMatchKeyword("record_type", "document_head"),
		qdrant.NewMatchKeyword("hive_id", iw.Cfg.HiveID),
	}}
	rows, err := iw.QdrantClient.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: iw.Cfg.ControlCollection, Filter: filter, Limit: qdrant.PtrOf(uint32(5000)), WithPayload: qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return OperationalStatus{}, errors.New("document synchronization state is unavailable")
	}
	count, err := iw.QdrantClient.Count(ctx, &qdrant.CountPoints{CollectionName: iw.Cfg.ControlCollection, Exact: qdrant.PtrOf(true), Filter: filter})
	if err != nil || count != uint64(len(rows)) {
		return OperationalStatus{}, errors.New("document synchronization state is incomplete")
	}
	latest := ""
	for _, row := range rows {
		if payloadString(row.Payload, "state", "") == "pending_delete" {
			report.PendingDocuments++
		}
		updated := payloadString(row.Payload, "updated_at", "")
		if parsed, parseErr := time.Parse(time.RFC3339Nano, updated); parseErr == nil {
			if latest == "" || parsed.After(mustParseTimestamp(latest)) {
				latest = parsed.UTC().Format(time.RFC3339Nano)
			}
		}
	}
	if latest != "" {
		report.LastSynchronization = latest
	}
	return report, nil
}

func mustParseTimestamp(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func (iw *IngestionWorker) ValidateOperational(ctx context.Context) ValidationReport {
	report := ValidationReport{OK: true, ExitCode: ExitOK, Checks: []ValidationCheck{}}
	add := func(name string, err error) {
		check := ValidationCheck{Name: name, Status: "passed"}
		if err != nil {
			report.OK = false
			check.Status = "failed"
			check.Detail = sanitizeOperationalError(err)
			code := classifyOperationalError(err)
			if report.ExitCode == ExitOK || code < report.ExitCode {
				report.ExitCode = code
			}
		}
		report.Checks = append(report.Checks, check)
	}
	add("configuration", validateOperationalConfig(iw.Cfg))
	if iw.Cfg.IsWriter() {
		_, err := validateDataDirectory(iw.Cfg.DataDirectory)
		add("writer_data_directory", err)
	}

	var qdrantErr error
	for _, collection := range []string{iw.Cfg.CollectionName, iw.Cfg.ControlCollection} {
		if _, err := iw.QdrantClient.CollectionExists(ctx, collection); err != nil {
			qdrantErr = err
			break
		}
	}
	add("qdrant_connectivity_and_tls", qdrantErr)

	var infrastructureErr error
	if iw.Cfg.IsWriter() {
		infrastructureErr = iw.EnsureInfrastructure(ctx)
	} else {
		infrastructureErr = iw.ValidateInfrastructure(ctx)
	}
	add("infrastructure_and_fingerprint", infrastructureErr)
	if infrastructureErr == nil && qdrantErr == nil {
		add("credential_capabilities", iw.ValidateCredentialCapabilities(ctx))
	} else {
		report.Checks = append(report.Checks, ValidationCheck{Name: "credential_capabilities", Status: "skipped", Detail: "prerequisite validation failed"})
	}
	return report
}

func validateOperationalConfig(cfg Config) error {
	if !validIdentifier(cfg.HiveID, 64) || !validIdentifier(cfg.DeviceID, 64) ||
		(cfg.Role != RoleWriter && cfg.Role != RoleReader) || cfg.CollectionName == "" ||
		!cfg.QdrantAPIKey.IsSet() {
		return errors.New("configuration is invalid")
	}
	if !cfg.QdrantUseTLS && !isLoopbackHost(cfg.QdrantHost) {
		return errors.New("TLS is required for remote Qdrant")
	}
	return nil
}

func classifyOperationalError(err error) int {
	if err == nil {
		return ExitOK
	}
	code := status.Code(err)
	if code == codes.Unauthenticated || code == codes.PermissionDenied {
		return ExitAuthorization
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "configuration"):
		return ExitConfiguration
	case strings.Contains(message, "credential"), strings.Contains(message, "permission"), strings.Contains(message, "authorization"), strings.Contains(message, "authenticated"):
		return ExitAuthorization
	case strings.Contains(message, "tls"), strings.Contains(message, "certificate"), strings.Contains(message, "x509"):
		return ExitTLS
	case strings.Contains(message, "fingerprint"), strings.Contains(message, "incompatible"), strings.Contains(message, "dimension"), strings.Contains(message, "schema"), strings.Contains(message, "manifest field"):
		return ExitCompatibility
	case strings.Contains(message, "partial"), strings.Contains(message, "cleanup pending"), strings.Contains(message, "incomplete"):
		return ExitPartialFailure
	default:
		return ExitConnectivity
	}
}

func sanitizeOperationalError(err error) string {
	code := classifyOperationalError(err)
	switch code {
	case ExitConfiguration:
		return "configuration is invalid"
	case ExitAuthorization:
		return "Qdrant authentication or authorization failed"
	case ExitTLS:
		return "Qdrant TLS validation failed"
	case ExitCompatibility:
		return "embedding or collection schema is incompatible"
	case ExitPartialFailure:
		return "operation is incomplete and recoverable"
	default:
		return "required service is unavailable"
	}
}

func parseSearchCLI(args []string) (HiveSearchArguments, error) {
	if len(args) < 2 {
		return HiveSearchArguments{}, errors.New("search requires program_id and query")
	}
	result := HiveSearchArguments{ProgramID: args[0]}
	var query []string
	for _, arg := range args[1:] {
		name, value, isFlag := strings.Cut(arg, "=")
		if !isFlag || !strings.HasPrefix(name, "--") {
			query = append(query, arg)
			continue
		}
		switch name {
		case "--document-type":
			result.DocumentTypes = append(result.DocumentTypes, value)
		case "--tag":
			result.Tags = append(result.Tags, value)
		case "--classification":
			result.Classification = value
		case "--scope-status":
			result.EffectiveScopeStatus = value
		case "--limit":
			limit, err := parsePositiveInt(value)
			if err != nil {
				return HiveSearchArguments{}, errors.New("invalid --limit")
			}
			result.Limit = &limit
		default:
			return HiveSearchArguments{}, errors.New("unknown search flag")
		}
	}
	result.Query = strings.Join(query, " ")
	return result, nil
}

func parsePositiveInt(value string) (int, error) {
	if value == "" {
		return 0, errors.New("empty integer")
	}
	result := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, errors.New("invalid integer")
		}
		result = result*10 + int(char-'0')
	}
	return result, nil
}

func sortedPendingPaths(paths map[string]time.Time) []string {
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
