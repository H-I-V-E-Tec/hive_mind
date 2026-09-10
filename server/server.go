package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/fsnotify/fsnotify"
	"github.com/qdrant/go-client/qdrant"
)

var Version = "1.0.0"

func Start(version string) {
	Version = version

	// Setup localized logs redirected away from stdout to keep MCP channel clean
	log.SetOutput(os.Stderr)
	if len(os.Args) > 1 && (os.Args[1] == "help" || os.Args[1] == "-h" || os.Args[1] == "--help") {
		printCLIHelp()
		return
	}

	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(10)
	}

	// Configure physical log file if option enabled by a future operational spec.
	if cfg.LogToFile {
		dirPath := ".qdrant-mcp-server"
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			log.Printf("Warning: Failed to create log directory '%s': %v", dirPath, err)
		}
		logFilePath := filepath.Join(dirPath, "qdrant-mcp-server.log")
		logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Printf("Warning: Failed to open log file '%s': %v", logFilePath, err)
		} else {
			log.SetOutput(logFile)
			log.Println("--- Log Session Started ---")
		}
	}

	// Intercept command line arguments for skill generation
	if len(os.Args) > 1 {
		cmd := strings.ToLower(os.Args[1])
		switch cmd {
		case "list-skills", "-list", "--list":
			ListSkills()
			return
		case "install-skill", "-install", "--install", "install":
			if len(os.Args) < 3 {
				fmt.Fprintln(os.Stderr, "Error: missing agent name.")
				fmt.Fprintln(os.Stderr, "Usage: qdrant-mcp-server install-skill <agent|all> [destination_directory]")
				os.Exit(1)
			}
			agent := os.Args[2]
			destDir := ""
			if len(os.Args) > 3 {
				destDir = os.Args[3]
			}
			if err := InstallSkill(agent, destDir); err != nil {
				fmt.Fprintf(os.Stderr, "Error installing skill: %v\n", err)
				os.Exit(1)
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

			log.Println("Starting manual codebase ingestion...")
			count, err := worker.SyncWorkspace(context.Background())
			if err != nil {
				log.Fatalf("Error during manual ingestion: %v", err)
			}
			fmt.Printf("🎉 Success! Ingested %d files into collection '%s'.\n", count, cfg.CollectionName)
			return
		case "search", "-search", "--search":
			if len(os.Args) < 3 {
				fmt.Fprintln(os.Stderr, "Error: missing query.")
				fmt.Fprintln(os.Stderr, "Usage: qdrant-mcp-server search <query>")
				os.Exit(1)
			}
			client, worker := mustCreateWorker(cfg)
			defer client.Close()
			defer worker.Close()

			query := strings.Join(os.Args[2:], " ")
			results, err := worker.ExecuteVectorSearch(context.Background(), query, nil, "")
			if err != nil {
				log.Fatalf("Search failed: %v", err)
			}
			fmt.Println(results)
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
				result, err := worker.ExecuteVectorSearch(context.Background(), suite.Query, suite.FileExtensions, suite.PathPrefix)
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

	client, err := newQdrantClient(cfg)
	if err != nil {
		log.Fatalf("Failed to establish Qdrant connection: %v", err)
	}
	defer client.Close()

	var gitIgnore *GitIgnoreMatcher
	if cfg.IsWriter() {
		gitIgnore = NewGitIgnoreMatcher(cfg.WatchDirectory)
	}

	worker := NewIngestionWorker(cfg, client, gitIgnore)
	defer worker.Close()
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
	client, err := newQdrantClient(cfg)
	if err != nil {
		log.Fatalf("Failed to establish Qdrant connection: %v", err)
	}

	var gitIgnore *GitIgnoreMatcher
	if cfg.IsWriter() {
		gitIgnore = NewGitIgnoreMatcher(cfg.WatchDirectory)
	}
	worker := NewIngestionWorker(cfg, client, gitIgnore)
	return client, worker
}

func newQdrantClient(cfg Config) (*qdrant.Client, error) {
	tlsConfig, err := cfg.QdrantTLSConfig()
	if err != nil {
		return nil, err
	}
	return qdrant.NewClient(&qdrant.Config{
		Host:      cfg.QdrantHost,
		Port:      cfg.QdrantPort,
		APIKey:    cfg.QdrantAPIKey.Reveal(),
		UseTLS:    cfg.QdrantUseTLS,
		TLSConfig: tlsConfig,
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
	fmt.Println("  ingest                         Ingest HIVE_DATA_DIR (writer only).")
	fmt.Println("  search <query>                 Execute a direct semantic search from the CLI.")
	fmt.Println("  evaluate-search                Ingest the workspace and run canned search quality checks.")
	fmt.Println("  list-skills                    List all available AI agent skills.")
	fmt.Println("  install-skill <agent> [dir]    Installs the rules file for the specified agent.")
	fmt.Println("                                 Options: cursor, windsurf, cline, copilot, generic, codex, all.")
	fmt.Println("  help, -h, --help               Show this help information.")
	fmt.Println()
	fmt.Println("Configuration flags (override environment and --config):")
	fmt.Println("  --config <path>                Read the explicitly selected flat TOML file.")
	fmt.Println("  --hive-id <id>                 Hive identifier.")
	fmt.Println("  --device-id <id>               Authorized device identifier.")
	fmt.Println("  --role <writer|reader>         Immutable process role.")
	fmt.Println("  --collection <name>            Qdrant data collection.")
	fmt.Println("  --data-dir <path>              Hive data directory (writer only).")
	fmt.Println("  --qdrant-url <url>             Qdrant URL; HTTPS required off loopback.")
	fmt.Println("  --qdrant-tls-ca-file <path>    Optional private CA bundle.")
	fmt.Println("  --qdrant-tls-server-name <id>  Optional expected certificate name.")
	fmt.Println("  --ollama-url <url>             Loopback Ollama URL.")
	fmt.Println("  --embedding-model <name>       Ollama embedding model.")
	fmt.Println("  --max-classification <level>   internal (default) or restricted.")
	fmt.Println()
	fmt.Println("Required environment/TOML keys:")
	fmt.Println("  HIVE_ID, HIVE_DEVICE_ID, HIVE_ROLE, HIVE_COLLECTION")
	fmt.Println("  QDRANT_URL, QDRANT_API_KEY, OLLAMA_URL, EMBEDDING_MODEL")
	fmt.Println("  HIVE_DATA_DIR is required only for writer.")
	fmt.Println()
	fmt.Println("QDRANT_API_KEY is never accepted as a process argument. Configuration files")
	fmt.Println("containing it must be permission-restricted and outside HIVE_DATA_DIR.")
	fmt.Println("No configuration file is auto-discovered and legacy keys are rejected.")
}
