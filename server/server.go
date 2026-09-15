package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
)

var Version = "1.0.0"
var SourceRevision = "development"

func Start(version string) {
	Version = version
	args, err := splitCLIArgs(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid command arguments")
		os.Exit(ExitUsage)
	}

	// Setup localized logs redirected away from stdout to keep MCP channel clean
	log.SetOutput(os.Stderr)
	if len(args) > 1 && (args[1] == "help" || args[1] == "-h" || args[1] == "--help") {
		printCLIHelp()
		return
	}
	if len(args) > 1 && args[1] == "inventory" {
		opts, err := parseInventoryArgs(args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid inventory arguments")
			os.Exit(ExitUsage)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		report := BuildInventory(ctx, opts)
		stop()
		printJSON(report)
		if !report.OK {
			os.Exit(report.ExitCode)
		}
		return
	}

	cfg, err := LoadConfig()
	if err != nil {
		printJSON(ValidationReport{OK: false, ExitCode: ExitConfiguration, Checks: []ValidationCheck{{Name: "configuration", Status: "failed", Detail: "configuration is invalid; check required Hive settings"}}})
		os.Exit(ExitConfiguration)
	}

	// Intercept command line arguments for skill generation
	if len(args) > 1 {
		cmd := strings.ToLower(args[1])
		switch cmd {
		case "audit":
			if cfg.AuditDirectory == "" {
				fmt.Fprintln(os.Stderr, "HIVE_AUDIT_DIR required for audit commands")
				os.Exit(ExitConfiguration)
			}
			sub := ""
			if len(args) > 2 {
				sub = strings.ToLower(args[2])
			}
			switch sub {
			case "record":
				if len(args) != 5 {
					fmt.Fprintln(os.Stderr, "Usage: qdrant-mcp-server audit record <credential_rotation|credential_revocation|writer_promotion> <change_id>")
					os.Exit(ExitUsage)
				}
				a, err := OpenFileAudit(cfg)
				if err != nil {
					printOperationalFailure("audit", err)
				}
				if err := a.Record(AuditEvent{Action: args[3], Outcome: "operator_recorded", ChangeID: args[4]}); err != nil {
					_ = a.Close()
					printOperationalFailure("audit", err)
				}
				if err := a.Close(); err != nil {
					printOperationalFailure("audit", err)
				}
				printJSON(map[string]any{"ok": true, "action": args[3], "change_id": args[4]})
			case "report":
				since, program, err := parseAuditReportArgs(args[3:])
				if err != nil {
					fmt.Fprintf(os.Stderr, "audit report input error: %v\n", err)
					fmt.Fprintln(os.Stderr, "Usage: qdrant-mcp-server audit report [--since=YYYY-MM-DD] [--program=<program_id>]")
					os.Exit(ExitUsage)
				}
				report, err := BuildAuditUsageReport(cfg.AuditDirectory, since, program)
				if err != nil {
					printOperationalFailure("audit report", err)
				}
				printJSON(report)
			default:
				fmt.Fprintln(os.Stderr, "Usage: qdrant-mcp-server audit <record|report> ...")
				os.Exit(ExitUsage)
			}
			return
		case "status":
			client, worker, err := createWorker(cfg)
			if err != nil {
				printOperationalFailure("status", err)
			}
			if err := validateStatusStartup(context.Background(), worker); err != nil {
				failCommand(client, worker, "status", err)
			}
			report, err := worker.OperationalStatus(context.Background())
			if err != nil {
				failCommand(client, worker, "status", err)
			}
			printJSON(report)
			worker.Close()
			client.Close()
			return
		case "validate":
			client, worker, err := createWorker(cfg)
			if err != nil {
				printValidationError(err)
			}
			report := worker.ValidateOperational(context.Background())
			printJSON(report)
			worker.Close()
			client.Close()
			if !report.OK {
				os.Exit(report.ExitCode)
			}
			return
		case "list-skills", "-list", "--list":
			ListSkills()
			return
		case "install-skill", "-install", "--install", "install":
			if len(args) < 3 {
				fmt.Fprintln(os.Stderr, "Error: missing agent name.")
				fmt.Fprintln(os.Stderr, "Usage: qdrant-mcp-server install-skill <agent|all> [destination_directory]")
				os.Exit(ExitUsage)
			}
			agent := args[2]
			destDir := ""
			if len(args) > 3 {
				destDir = args[3]
			}
			if err := InstallSkill(agent, destDir); err != nil {
				fmt.Fprintf(os.Stderr, "Error installing skill: %v\n", err)
				os.Exit(ExitPartialFailure)
			}
			return
		case "ingest", "-ingest", "--ingest":
			if !cfg.IsWriter() {
				fmt.Fprintln(os.Stderr, "authorization error: ingest requires HIVE_ROLE=writer")
				os.Exit(12)
			}
			client, worker := mustCreateWorker(cfg)
			defer client.Close()
			defer worker.Close()

			log.Println("Starting manual Hive ingestion...")
			report := worker.IngestWorkspaceReport(context.Background(), sliceContains(args[2:], "--prune"))
			printJSON(report)
			if !report.OK {
				worker.Close()
				client.Close()
				os.Exit(report.ExitCode)
			}
			return
		case "remove":
			if !cfg.IsWriter() {
				fmt.Fprintln(os.Stderr, "authorization error: remove requires HIVE_ROLE=writer")
				os.Exit(12)
			}
			if len(args) != 3 {
				fmt.Fprintln(os.Stderr, "Usage: qdrant-mcp-server remove <path>")
				os.Exit(ExitUsage)
			}
			client, worker := mustCreateWorker(cfg)
			defer client.Close()
			defer worker.Close()
			if err := worker.RemoveDocument(context.Background(), args[2], "operator_requested"); err != nil {
				code := classifyOperationalError(err)
				failCommandWithCode(client, worker, "remove", err, code)
			}
			fmt.Println("Document tombstoned and removed.")
			return
		case "scope":
			if !cfg.IsWriter() {
				fmt.Fprintln(os.Stderr, "authorization error: scope operations require HIVE_ROLE=writer")
				os.Exit(12)
			}
			if (len(args) != 4 && len(args) != 5) || strings.ToLower(args[2]) != "approve" || (len(args) == 5 && args[4] != "--yes") {
				fmt.Fprintln(os.Stderr, "Usage: qdrant-mcp-server scope approve <program_id> [--yes]")
				os.Exit(ExitUsage)
			}
			previewWorker := &IngestionWorker{Cfg: cfg}
			summary, err := previewWorker.ScopeApprovalPreview(args[3])
			if err != nil {
				fmt.Fprintf(os.Stderr, "scope approval input error: %s\n", sanitizeCLIInputError(err))
				os.Exit(ExitUsage)
			}
			printJSON(summary)
			if len(args) != 5 {
				fmt.Fprintln(os.Stderr, "Approval not applied. Review the hash and repeat with --yes to confirm explicitly.")
				os.Exit(ExitUsage)
			}
			client, worker := mustCreateWorker(cfg)
			defer client.Close()
			defer worker.Close()
			if err := worker.ApproveScopeRevision(context.Background(), args[3], summary.SHA256); err != nil {
				failCommand(client, worker, "scope approval", err)
			}
			fmt.Printf("Scope manifest approved for program %q.\n", args[3])
			return
		case "search", "-search", "--search":
			if len(args) < 4 {
				fmt.Fprintln(os.Stderr, "Error: missing program_id or query.")
				fmt.Fprintln(os.Stderr, "Usage: qdrant-mcp-server search <program_id> <query> [--document-type=value] [--tag=value] [--classification=value] [--scope-status=value] [--limit=N]")
				os.Exit(ExitUsage)
			}
			searchArgs, err := parseSearchCLI(args[2:])
			if err == nil {
				_, _, err = (&IngestionWorker{Cfg: cfg}).validateHiveSearch(searchArgs)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "search input error: %v\n", err)
				os.Exit(ExitUsage)
			}
			client, worker := mustCreateWorker(cfg)
			defer client.Close()
			defer worker.Close()

			results, err := worker.HiveSearch(context.Background(), searchArgs)
			if err != nil {
				code := classifyOperationalError(err)
				if isSearchValidationError(err) {
					code = ExitUsage
				}
				failCommandWithCode(client, worker, "search", err, code)
			}
			encoded, err := json.MarshalIndent(results, "", "  ")
			if err != nil {
				failCommandWithCode(client, worker, "search", err, ExitPartialFailure)
			}
			fmt.Println(string(encoded))
			return
		case "evaluate-search", "eval-search", "eval":
			if !cfg.IsWriter() {
				fmt.Fprintln(os.Stderr, "authorization error: evaluate-search requires HIVE_ROLE=writer")
				os.Exit(12)
			}
			client, worker := mustCreateWorker(cfg)
			defer client.Close()
			defer worker.Close()

			log.Printf("Running self-evaluation against workspace %s", cfg.WatchDirectory)
			if _, err := worker.SyncWorkspace(context.Background()); err != nil {
				log.Fatalf("Self-ingestion failed before evaluation: %v", err)
			}

			suites := defaultEvaluationQueries(cfg.WatchDirectory)
			passed := 0
			for _, suite := range suites {
				result, err := worker.ExecuteVectorSearch(context.Background(), "default", suite.Query, suite.FileExtensions, suite.PathPrefix)
				if err != nil {
					log.Printf("Evaluation query failed for %q: %v", suite.Query, err)
					continue
				}
				ok := strings.Contains(strings.ToLower(result), strings.ToLower(suite.ExpectContains))
				if ok {
					passed++
				}
				fmt.Printf("\n=== Query: %s ===\nExpected: %s\nPass: %t\n\n%s\n", suite.Query, suite.ExpectContains, ok, result)
			}
			fmt.Printf("\nEvaluation summary: %d/%d queries matched expected fragments.\n", passed, len(suites))
			return
		case "help", "-h", "--help":
			printCLIHelp()
			return
		}
	}

	log.Println("Starting Go Qdrant-RAG MCP Server...")

	client, worker, err := createWorker(cfg)
	if err != nil {
		log.Fatalf("Failed to establish Qdrant connection: %v", err)
	}
	defer client.Close()

	defer worker.Close()
	if err := validateWorkerStartup(context.Background(), worker); err != nil {
		log.Fatalf("Hive startup validation failed: %v", err)
	}
	if cfg.IsReader() {
		worker.ListenToMCPClient(context.Background())
		return
	}

	// Boot active structural watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to spin up filesystem notification systems: %v", err)
	}
	defer watcher.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancelHandle(cancel)

	// Spawn decoupled debounced file processor
	eventChan := make(chan string, 100)
	go worker.WatchLoop(ctx, watcher, eventChan)
	go worker.IngestionConsumer(ctx, eventChan)

	// Recursively monitor target codebase structure
	err = worker.addWatchRecursive(watcher, cfg.WatchDirectory)
	if err != nil {
		log.Printf("Warning: Directory traversal hit path restrictions: %v", err)
	}

	// Launch standard MCP protocol engine on main thread
	go worker.ListenToMCPClient(ctx)

	// Block gracefully until system signal caught
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("Shutting down Go MCP Server cleanly.")
}

func cancelHandle(c context.CancelFunc) { c() }

type EvaluationQuery struct {
	Query          string
	ExpectContains string
	FileExtensions []string
	PathPrefix     string
}

func mustCreateWorker(cfg Config) (*qdrant.Client, *IngestionWorker) {
	client, worker, err := createWorker(cfg)
	if err != nil {
		printOperationalFailure("Qdrant client", err)
	}
	if err := validateWorkerStartup(context.Background(), worker); err != nil {
		failCommand(client, worker, "Hive infrastructure validation", err)
	}
	return client, worker
}

func createWorker(cfg Config) (*qdrant.Client, *IngestionWorker, error) {
	if cfg.AuditDirectory == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return nil, nil, &operationalError{ExitConfiguration, "HIVE_AUDIT_DIR is required"}
		}
		cfg.AuditDirectory = filepath.Join(cache, "hive-mind", "audit")
	}
	audit, err := OpenFileAudit(cfg)
	if err != nil {
		return nil, nil, &operationalError{ExitConfiguration, "audit storage configuration is invalid"}
	}
	client, err := newQdrantClient(cfg)
	if err != nil {
		_ = audit.Close()
		return nil, nil, err
	}
	var gitIgnore *GitIgnoreMatcher
	if cfg.IsWriter() {
		gitIgnore = NewGitIgnoreMatcher(cfg.WatchDirectory)
	}
	worker := NewIngestionWorker(cfg, client, gitIgnore)
	worker.Audit = audit
	worker.auditOwned = true
	log.SetOutput(privateDiagnosticWriter{worker})
	return client, worker, nil
}

func printJSON(value any) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "output encoding failed")
		os.Exit(ExitPartialFailure)
	}
	fmt.Println(string(encoded))
}

func printOperationalFailure(operation string, err error) {
	code := classifyOperationalError(err)
	fmt.Fprintf(os.Stderr, "%s failed: %s\n", operation, sanitizeOperationalError(err))
	os.Exit(code)
}

func printValidationError(err error) {
	code := classifyOperationalError(err)
	printJSON(ValidationReport{OK: false, ExitCode: code, Checks: []ValidationCheck{{Name: "qdrant_client", Status: "failed", Detail: sanitizeOperationalError(err)}}})
	os.Exit(code)
}

func failCommand(client *qdrant.Client, worker *IngestionWorker, operation string, err error) {
	failCommandWithCode(client, worker, operation, err, classifyOperationalError(err))
}

func failCommandWithCode(client *qdrant.Client, worker *IngestionWorker, operation string, err error, code int) {
	if worker != nil {
		worker.Close()
	}
	if client != nil {
		client.Close()
	}
	fmt.Fprintf(os.Stderr, "%s failed: %s\n", operation, sanitizeOperationalErrorForCode(err, code))
	os.Exit(code)
}

func sanitizeOperationalErrorForCode(err error, code int) string {
	if code == ExitUsage {
		return sanitizeCLIInputError(err)
	}
	return sanitizeOperationalError(err)
}

func sanitizeCLIInputError(err error) string {
	return "invalid input; check the command arguments and document format"
}

func validateWorkerStartup(ctx context.Context, worker *IngestionWorker) error {
	var err error
	if worker.Cfg.IsWriter() {
		err = worker.EnsureInfrastructure(ctx)
	} else {
		err = worker.ValidateInfrastructure(ctx)
	}
	if err != nil {
		return err
	}
	return worker.ValidateCredentialCapabilities(ctx)
}

func validateStatusStartup(ctx context.Context, worker *IngestionWorker) error {
	if err := worker.ValidateInfrastructure(ctx); err != nil {
		return err
	}
	return worker.ValidateCredentialCapabilities(ctx)
}

func newQdrantClient(cfg Config) (*qdrant.Client, error) {
	return newQdrantClientWithOptions(cfg, nil)
}

func newQdrantClientWithOptions(cfg Config, options []grpc.DialOption) (*qdrant.Client, error) {
	tlsConfig, err := cfg.QdrantTLSConfig()
	if err != nil {
		return nil, err
	}
	return qdrant.NewClient(&qdrant.Config{
		PoolSize:               1,
		SkipCompatibilityCheck: true,
		GrpcOptions:            append([]grpc.DialOption{grpc.WithChainUnaryInterceptor(qdrantRetryUnaryInterceptor(serviceRetryPolicy))}, options...),
		Host:                   cfg.QdrantHost,
		Port:                   cfg.QdrantPort,
		APIKey:                 cfg.QdrantAPIKey.Reveal(),
		UseTLS:                 cfg.QdrantUseTLS,
		TLSConfig:              tlsConfig,
	})
}

func defaultEvaluationQueries(watchDir string) []EvaluationQuery {
	return []EvaluationQuery{
		{Query: "recursive watcher for newly created directories", ExpectContains: "server/watcher.go", FileExtensions: []string{"go"}, PathPrefix: "server"},
		{Query: "vector search execution and reranking", ExpectContains: "server/worker.go", FileExtensions: []string{"go"}, PathPrefix: "server"},
		{Query: "auto discover mcp and codex config", ExpectContains: "server/config.go", FileExtensions: []string{"go"}, PathPrefix: "server"},
		{Query: "tree sitter parse code metadata and imports", ExpectContains: "ast/ast.go", FileExtensions: []string{"go"}, PathPrefix: "ast"},
		{Query: "worker tags and search tests", ExpectContains: "tests/worker_test.go", FileExtensions: []string{"go"}, PathPrefix: "tests"},
	}
}

func printCLIHelp() {
	fmt.Println("Hive Mind MCP")
	fmt.Println("Private shared recon memory backed by Qdrant and local Ollama.")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  (no arguments)                 Starts the active MCP server.")
	fmt.Println("  ingest [--prune]               Ingest HIVE_DATA_DIR; report per-file outcomes as JSON (partial failure: 15).")
	fmt.Println("                                Prune expired pending deletes only after a successful sync.")
	fmt.Println("  inventory <dir>                Offline JSON inventory of formats, sizes and byte-identical copies.")
	fmt.Println("    [--program=<id>] [--details]  Restrict to a program; opt in to relative paths. No Hive config needed.")
	fmt.Println("    [--hash-max-bytes=<n>]        Hash files up to n bytes (default 50 MiB; ceiling 1 GiB).")
	fmt.Println("  remove <path>                  Tombstone and remove one document (writer only).")
	fmt.Println("  status                         Show sanitized configuration and synchronization state.")
	fmt.Println("  validate                       Verify role, services, permissions, TLS and fingerprint.")
	fmt.Println("  audit record <event> <change>  Record credential_rotation, credential_revocation or writer_promotion.")
	fmt.Println("  audit report [--since=DATE]    Aggregate retrieval usage (queries, response chars, source bytes)")
	fmt.Println("               [--program=<id>]  from local audit logs; numbers only, no content.")
	fmt.Println("  scope approve <program_id>     Preview the manifest hash; add --yes to approve it.")
	fmt.Println("  search <program_id> <query>    Search; filters use --tag=value and related flags.")
	fmt.Println("  list-skills                    List all available AI agent skills.")
	fmt.Println("  install-skill <agent> [dir]    Installs the rules file for the specified agent.")
	fmt.Println("                                 Options: claude, codex (maintained); cursor, windsurf,")
	fmt.Println("                                 cline, copilot, generic (legacy); all.")
	fmt.Println("  help, -h, --help               Show this help information.")
	fmt.Println()
	fmt.Println("Configuration flags (override environment and --config):")
	fmt.Println("  --config <path>                Read the explicitly selected flat TOML file.")
	fmt.Println("  --hive-id <id>                 Hive identifier.")
	fmt.Println("  --device-id <id>               Authorized device identifier.")
	fmt.Println("  --writer-approval-id <id>      Sanitized operational change/ticket for the writer.")
	fmt.Println("  --role <writer|reader>         Immutable process role.")
	fmt.Println("  --collection <name>            Qdrant data collection.")
	fmt.Println("  --data-dir <path>              Hive data directory (writer only).")
	fmt.Println("  --qdrant-url <url>             Qdrant URL; HTTPS required off loopback.")
	fmt.Println("  --qdrant-tls-ca-file <path>    Optional private CA bundle.")
	fmt.Println("  --qdrant-tls-server-name <id>  Optional expected certificate name.")
	fmt.Println("  --ollama-url <url>             Loopback Ollama URL.")
	fmt.Println("  --embedding-model <name>       Ollama embedding model.")
	fmt.Println("  --max-classification <level>   internal (default) or restricted.")
	fmt.Println("  --context-max-chars <n>        Serialized context budget (default 12000; range 1000-50000).")
	fmt.Println("  --max-file-bytes <n>           Maximum document bytes (default 5242880; ceiling 50 MiB).")
	fmt.Println("  --max-chunks-per-file <n>      Maximum chunks per document (default 1000; ceiling 5000).")
	fmt.Println("  --chunk-max-chars <n>          Chunk size in characters (default 2000; ceiling 8000).")
	fmt.Println("  --chunk-overlap-chars <n>      Text overlap, at most half the chunk size (default 200).")
	fmt.Println("  --max-embedding-workers <n>    Concurrent embedding calls (default 2; ceiling 16).")
	fmt.Println()
	fmt.Println("Required environment/TOML keys:")
	fmt.Println("  HIVE_ID, HIVE_DEVICE_ID, HIVE_ROLE, HIVE_COLLECTION")
	fmt.Println("  QDRANT_URL, QDRANT_API_KEY, OLLAMA_URL, EMBEDDING_MODEL")
	fmt.Println("  HIVE_DATA_DIR and HIVE_WRITER_APPROVAL_ID are required only for writer.")
	fmt.Println()
	fmt.Println("QDRANT_API_KEY is never accepted as a process argument. Configuration files")
	fmt.Println("containing it must be permission-restricted and outside HIVE_DATA_DIR.")
	fmt.Println("No configuration file is auto-discovered and legacy keys are rejected.")
}
