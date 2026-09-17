package server

import (
	"context"
	"errors"
	"net"
	"sort"
	"strconv"
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
	if iw.auditFailed.Load() {
		report.Warnings = append(report.Warnings, "audit unavailable; critical operations are blocked")
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
	configErr := validateOperationalConfig(iw.Cfg)
	add("configuration", configErr)
	if configErr != nil {
		return report
	}
	add("audit_storage", iw.audit(AuditEvent{Action: "validation", Outcome: "prepared"}, true))
	if iw.Cfg.IsWriter() {
		_, err := validateDataDirectory(iw.Cfg.DataDirectory)
		if err != nil {
			err = &operationalError{ExitConfiguration, "writer data directory is unavailable"}
		}
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

	infrastructureErr := iw.ValidateInfrastructure(ctx)
	add("infrastructure_and_fingerprint", infrastructureErr)
	if qdrantErr == nil {
		add("credential_capabilities", iw.ValidateCredentialCapabilities(ctx))
	} else {
		report.Checks = append(report.Checks, ValidationCheck{Name: "credential_capabilities", Status: "skipped", Detail: "prerequisite validation failed"})
	}
	outcome := "passed"
	if !report.OK {
		outcome = "failed"
	}
	add("audit_validation", iw.audit(AuditEvent{Action: "validation", Outcome: outcome}, true))
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
	var coded *operationalError
	if errors.As(err, &coded) {
		return coded.code
	}
	code := status.Code(err)
	if code == codes.Unauthenticated || code == codes.PermissionDenied {
		return ExitAuthorization
	}
	if code == codes.Unavailable || code == codes.DeadlineExceeded {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "tls") || strings.Contains(message, "certificate") || strings.Contains(message, "x509") {
			return ExitTLS
		}
		return ExitConnectivity
	}
	message := strings.ToLower(err.Error())
	var networkError net.Error
	if errors.As(err, &networkError) && !strings.Contains(message, "certificate") {
		return ExitConnectivity
	}
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
		if strings.HasPrefix(arg, "--") && !isFlag {
			return result, errors.New("search flags require --name=value")
		}
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

func splitCLIArgs(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, errors.New("missing invocation")
	}
	if len(raw) > 1 && raw[1] == "convert" {
		if _, err := parseConvertArgs(raw[2:]); err != nil {
			return nil, err
		}
		return raw, nil
	}
	// Offline inventory has its own explicit options; never silently strip or
	// load service configuration flags for this command.
	if len(raw) > 1 && raw[1] == "inventory" {
		if _, err := parseInventoryArgs(raw[2:]); err != nil {
			return nil, err
		}
		return raw, nil
	}
	args := []string{raw[0]}
	for i := 1; i < len(raw); i++ {
		name, _, inline := strings.Cut(raw[i], "=")
		_, config := configFlagKeys[name]
		if config || name == "--config" {
			if !inline {
				i++
				if i >= len(raw) || strings.HasPrefix(raw[i], "--") {
					return nil, errors.New("missing configuration value")
				}
			}
			continue
		}
		args = append(args, raw[i])
	}
	if len(args) == 1 {
		return args, nil
	}
	switch args[1] {
	case "audit":
		if len(args) < 3 {
			return nil, errors.New("invalid audit command")
		}
		switch args[2] {
		case "record":
			if len(args) != 5 || !validIdentifier(args[4], 64) {
				return nil, errors.New("invalid audit command")
			}
			if args[3] != "credential_rotation" && args[3] != "credential_revocation" && args[3] != "writer_promotion" {
				return nil, errors.New("invalid audit event")
			}
		case "report":
			if _, _, err := parseAuditReportArgs(args[3:]); err != nil {
				return nil, errors.New("invalid audit report arguments")
			}
		default:
			return nil, errors.New("invalid audit command")
		}
	case "help", "-h", "--help", "status", "validate", "list-skills":
		if len(args) != 2 {
			return nil, errors.New("unexpected arguments")
		}
	case "ingest":
		if len(args) > 3 || (len(args) == 3 && args[2] != "--prune") {
			return nil, errors.New("invalid ingest arguments")
		}
	case "remove":
		if len(args) != 3 {
			return nil, errors.New("remove requires a path")
		}
	case "scope":
		if (len(args) != 4 && len(args) != 5) || args[2] != "approve" || (len(args) == 5 && args[4] != "--yes") {
			return nil, errors.New("invalid scope arguments")
		}
	case "search":
		if len(args) < 4 {
			return nil, errors.New("search requires program and query")
		}
	case "install-skill":
		if len(args) < 3 || len(args) > 4 {
			return nil, errors.New("invalid install arguments")
		}
	default:
		return nil, errors.New("unknown command")
	}
	return args, nil
}

func parsePositiveInt(value string) (int, error) {
	result, err := strconv.ParseUint(value, 10, 31)
	if err != nil || result == 0 {
		return 0, errors.New("invalid positive integer")
	}
	return int(result), nil
}

// Preserve machine-readable failure categories without propagating service
// messages, which may contain credentials, content or private endpoints.
type operationalError struct {
	code    int
	message string
}

func (e *operationalError) Error() string { return e.message }
func safeServiceError(err error) error {
	return &operationalError{classifyOperationalError(err), sanitizeOperationalError(err)}
}

func sortedPendingPaths(paths map[string]time.Time) []string {
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}
