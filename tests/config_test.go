package tests

import (
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"qdrant-mcp-server/server"
)

const sentinelAPIKey = "spec01-sentinel-api-key"

func validHiveEnv(dataDir string) map[string]string {
	return map[string]string{
		"HIVE_ID":                 "research-team",
		"HIVE_DEVICE_ID":          "workstation-a",
		"HIVE_WRITER_APPROVAL_ID": "change-1042",
		"HIVE_ROLE":               "writer",
		"HIVE_COLLECTION":         "hive_mind_v01",
		"HIVE_DATA_DIR":           dataDir,
		"QDRANT_URL":              "http://127.0.0.1:6334",
		"QDRANT_API_KEY":          sentinelAPIKey,
		"OLLAMA_URL":              "http://127.0.0.1:11434",
		"EMBEDDING_MODEL":         "nomic-embed-text",
	}
}

func cloneEnv(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func TestHiveConfigCompleteWriter(t *testing.T) {
	dataDir := t.TempDir()
	cfg, err := server.LoadConfigFrom(nil, validHiveEnv(dataDir))
	if err != nil {
		t.Fatalf("LoadConfigFrom returned error: %v", err)
	}
	if cfg.HiveID != "research-team" || cfg.DeviceID != "workstation-a" || !cfg.IsWriter() {
		t.Fatalf("unexpected identity or role: %+v", cfg)
	}
	if cfg.CollectionName != "hive_mind_v01" || cfg.ControlCollection != "hive_mind_v01__control" {
		t.Fatalf("unexpected collections: %+v", cfg)
	}
	if cfg.DataDirectory != dataDir || cfg.WatchDirectory != dataDir {
		t.Fatalf("data directory was not canonicalized as expected: %q", cfg.DataDirectory)
	}
	if cfg.ParserMode != "doc" || cfg.MaxEmbeddingWorkers != 2 || cfg.MaxFileSize != 5*1024*1024 {
		t.Fatalf("safe defaults were not applied: %+v", cfg)
	}
	if cfg.MaxChunksPerFile != 1000 || cfg.ChunkMaxChars != 2000 || cfg.ChunkOverlapChars != 200 || cfg.JSONMaxDepth != 64 || cfg.JSONMaxElements != 100000 || cfg.DeleteGrace != 24*time.Hour {
		t.Fatalf("spec-002 defaults were not applied: %+v", cfg)
	}
	if cfg.MaxClassification != "internal" {
		t.Fatalf("expected internal classification default, got %q", cfg.MaxClassification)
	}
}

func TestHiveConfigReaderDoesNotRequireDataDirectory(t *testing.T) {
	env := validHiveEnv(t.TempDir())
	env["HIVE_ROLE"] = "reader"
	delete(env, "HIVE_DATA_DIR")

	cfg, err := server.LoadConfigFrom(nil, env)
	if err != nil {
		t.Fatalf("reader config returned error: %v", err)
	}
	if !cfg.IsReader() || cfg.DataDirectory != "" {
		t.Fatalf("unexpected reader config: %+v", cfg)
	}
}

func TestHiveConfigBuildsVerifiedTLSSettings(t *testing.T) {
	env := validHiveEnv(t.TempDir())
	env["QDRANT_URL"] = "https://qdrant.hive.internal:7443"
	env["QDRANT_TLS_SERVER_NAME"] = "qdrant.internal.example"

	cfg, err := server.LoadConfigFrom(nil, env)
	if err != nil {
		t.Fatalf("TLS config returned error: %v", err)
	}
	if !cfg.QdrantUseTLS || cfg.QdrantHost != "qdrant.hive.internal" || cfg.QdrantPort != 7443 {
		t.Fatalf("unexpected Qdrant endpoint: %+v", cfg)
	}
	tlsConfig, err := cfg.QdrantTLSConfig()
	if err != nil {
		t.Fatalf("QdrantTLSConfig returned error: %v", err)
	}
	if tlsConfig.MinVersion != tls.VersionTLS12 || tlsConfig.ServerName != "qdrant.internal.example" || tlsConfig.InsecureSkipVerify {
		t.Fatalf("TLS verification is not fail-closed: %+v", tlsConfig)
	}
}

func TestHiveConfigPrecedenceFlagsEnvironmentFileDefaults(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	configPath := filepath.Join(root, "hive.toml")
	content := strings.Join([]string{
		`HIVE_ID = "from-file"`,
		`HIVE_DEVICE_ID = "file-device"`,
		`HIVE_ROLE = "writer"`,
		`HIVE_WRITER_APPROVAL_ID = "change-1042"`,
		`HIVE_COLLECTION = "file_collection"`,
		`HIVE_DATA_DIR = "` + dataDir + `"`,
		`QDRANT_URL = "http://127.0.0.1:6334"`,
		`QDRANT_API_KEY = "file-secret"`,
		`OLLAMA_URL = "http://127.0.0.1:11434"`,
		`EMBEDDING_MODEL = "file-model"`,
		`HIVE_MAX_CLASSIFICATION = "restricted"`,
	}, "\n")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	env := map[string]string{
		"HIVE_ID":         "from-environment",
		"QDRANT_API_KEY":  sentinelAPIKey,
		"EMBEDDING_MODEL": "environment-model",
	}
	args := []string{"--config", configPath, "--hive-id=from-flag"}
	cfg, err := server.LoadConfigFrom(args, env)
	if err != nil {
		t.Fatalf("LoadConfigFrom returned error: %v", err)
	}
	if cfg.HiveID != "from-flag" {
		t.Fatalf("flag did not win: %q", cfg.HiveID)
	}
	if cfg.EmbeddingModel != "environment-model" {
		t.Fatalf("environment did not win over file: %q", cfg.EmbeddingModel)
	}
	if cfg.DeviceID != "file-device" || cfg.MaxClassification != "restricted" {
		t.Fatalf("file values were not retained: %+v", cfg)
	}
}

func TestHiveConfigDoesNotAutoDiscover(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	if err := os.WriteFile(configPath, []byte(`HIVE_ID = "discovered"`), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	_, err = server.LoadConfigFrom(nil, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "HIVE_ID") {
		t.Fatalf("expected missing config instead of auto-discovery, got %v", err)
	}
}

func TestHiveConfigRejectsLegacyEnvironment(t *testing.T) {
	for _, legacyKey := range []string{
		"HIVE_MODE", "WATCH_DIRECTORY", "QDRANT_COLLECTION",
		"QDRANT_HOST", "QDRANT_PORT", "OLLAMA_HOST",
	} {
		t.Run(legacyKey, func(t *testing.T) {
			env := validHiveEnv(t.TempDir())
			env[legacyKey] = "legacy-value"
			_, err := server.LoadConfigFrom(nil, env)
			if err == nil || !strings.Contains(err.Error(), legacyKey) {
				t.Fatalf("expected explicit legacy rejection, got %v", err)
			}
		})
	}
}

func TestHiveConfigRejectsInvalidOrMissingValues(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(map[string]string)
		expected string
	}{
		{"missing hive", func(env map[string]string) { delete(env, "HIVE_ID") }, "HIVE_ID"},
		{"invalid hive", func(env map[string]string) { env["HIVE_ID"] = "Bad Hive" }, "HIVE_ID"},
		{"invalid device", func(env map[string]string) { env["HIVE_DEVICE_ID"] = "DEVICE" }, "HIVE_DEVICE_ID"},
		{"invalid role", func(env map[string]string) { env["HIVE_ROLE"] = "admin" }, "HIVE_ROLE"},
		{"long collection", func(env map[string]string) { env["HIVE_COLLECTION"] = strings.Repeat("a", 56) }, "HIVE_COLLECTION"},
		{"missing writer dir", func(env map[string]string) { delete(env, "HIVE_DATA_DIR") }, "HIVE_DATA_DIR"},
		{"missing writer approval", func(env map[string]string) { delete(env, "HIVE_WRITER_APPROVAL_ID") }, "HIVE_WRITER_APPROVAL_ID"},
		{"missing directory", func(env map[string]string) { env["HIVE_DATA_DIR"] = filepath.Join(t.TempDir(), "missing") }, "HIVE_DATA_DIR"},
		{"qdrant path", func(env map[string]string) { env["QDRANT_URL"] = "https://qdrant.example/private" }, "QDRANT_URL"},
		{"remote plaintext qdrant", func(env map[string]string) { env["QDRANT_URL"] = "http://10.0.0.10:6334" }, "https"},
		{"remote ollama", func(env map[string]string) { env["OLLAMA_URL"] = "http://10.0.0.11:11434" }, "loopback"},
		{"invalid classification", func(env map[string]string) { env["HIVE_MAX_CLASSIFICATION"] = "public" }, "HIVE_MAX_CLASSIFICATION"},
		{"excessive file bytes", func(env map[string]string) { env["HIVE_MAX_FILE_BYTES"] = "52428801" }, "HIVE_MAX_FILE_BYTES"},
		{"excessive chunks", func(env map[string]string) { env["HIVE_MAX_CHUNKS_PER_FILE"] = "5001" }, "HIVE_MAX_CHUNKS_PER_FILE"},
		{"overlap over half", func(env map[string]string) {
			env["HIVE_CHUNK_MAX_CHARS"] = "100"
			env["HIVE_CHUNK_OVERLAP_CHARS"] = "51"
		}, "HIVE_CHUNK_OVERLAP_CHARS"},
		{"excessive JSON depth", func(env map[string]string) { env["HIVE_JSON_MAX_DEPTH"] = "65" }, "HIVE_JSON_MAX_DEPTH"},
		{"excessive workers", func(env map[string]string) { env["HIVE_MAX_EMBEDDING_WORKERS"] = "17" }, "HIVE_MAX_EMBEDDING_WORKERS"},
		{"excessive delete grace", func(env map[string]string) { env["HIVE_DELETE_GRACE_HOURS"] = "25" }, "HIVE_DELETE_GRACE_HOURS"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := validHiveEnv(t.TempDir())
			test.mutate(env)
			_, err := server.LoadConfigFrom(nil, env)
			if err == nil || !strings.Contains(err.Error(), test.expected) {
				t.Fatalf("expected error containing %q, got %v", test.expected, err)
			}
		})
	}
}

func TestHiveConfigRejectsSecretInProcessArguments(t *testing.T) {
	_, err := server.LoadConfigFrom([]string{"--qdrant-api-key=" + sentinelAPIKey}, validHiveEnv(t.TempDir()))
	if err == nil || strings.Contains(err.Error(), sentinelAPIKey) {
		t.Fatalf("secret argument was not safely rejected: %v", err)
	}
}

func TestHiveConfigRejectsUnknownAndValuelessFlags(t *testing.T) {
	for _, args := range [][]string{{"--watch-dir", "/tmp"}, {"--hive-id"}, {"--config", "--role"}} {
		_, err := server.LoadConfigFrom(args, validHiveEnv(t.TempDir()))
		if err == nil {
			t.Fatalf("expected flag rejection for %v", args)
		}
	}
}

func TestHiveConfigSecretNeverAppearsInFormattingOrErrors(t *testing.T) {
	env := validHiveEnv(t.TempDir())
	cfg, err := server.LoadConfigFrom(nil, env)
	if err != nil {
		t.Fatal(err)
	}
	outputs := []string{
		fmt.Sprintf("%v", cfg), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg),
		fmt.Sprintf("%v", cfg.QdrantAPIKey), fmt.Sprintf("%+v", cfg.QdrantAPIKey),
	}
	for _, output := range outputs {
		if strings.Contains(output, sentinelAPIKey) {
			t.Fatalf("secret leaked through formatting: %s", output)
		}
	}

	bad := cloneEnv(env)
	bad["QDRANT_API_KEY"] = sentinelAPIKey + "\n"
	_, err = server.LoadConfigFrom(nil, bad)
	if err == nil || strings.Contains(err.Error(), sentinelAPIKey) {
		t.Fatalf("secret leaked through validation error: %v", err)
	}
}

func TestHiveConfigProtectedSecretFile(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.Mkdir(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := completeTOML(dataDir, sentinelAPIKey)

	outside := filepath.Join(root, "hive.toml")
	if err := os.WriteFile(outside, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := server.LoadConfigFrom([]string{"--config", outside}, nil); err != nil {
		t.Fatalf("protected file outside data dir should pass: %v", err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(outside, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := server.LoadConfigFrom([]string{"--config", outside}, nil); err == nil || !strings.Contains(err.Error(), "group or others") {
			t.Fatalf("expected permissive file rejection, got %v", err)
		}
		if _, err := server.LoadConfigFrom([]string{"--config", outside}, map[string]string{"QDRANT_API_KEY": "environment-override"}); err == nil || !strings.Contains(err.Error(), "group or others") {
			t.Fatalf("environment override must not make a secret-bearing file safe: %v", err)
		}
	}

	inside := filepath.Join(dataDir, "hive.toml")
	if err := os.WriteFile(inside, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := server.LoadConfigFrom([]string{"--config", inside}, nil); err == nil || !strings.Contains(err.Error(), "outside HIVE_DATA_DIR") {
		t.Fatalf("expected in-data-dir secret rejection, got %v", err)
	}
}

func TestHiveConfigRejectsUnknownOrDuplicateTOMLKeys(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{
		"unknown":   "UNKNOWN_SETTING = \"value\"\n",
		"duplicate": "HIVE_ID = \"one\"\nHIVE_ID = \"two\"\n",
		"section":   "[hive]\nHIVE_ID = \"one\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, name+".toml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := server.LoadConfigFrom([]string{"--config", path}, nil); err == nil {
				t.Fatalf("expected TOML rejection for %s", name)
			}
		})
	}
}

func completeTOML(dataDir, apiKey string) string {
	return strings.Join([]string{
		`HIVE_ID = "research-team"`,
		`HIVE_DEVICE_ID = "workstation-a"`,
		`HIVE_ROLE = "writer"`,
		`HIVE_WRITER_APPROVAL_ID = "change-1042"`,
		`HIVE_COLLECTION = "hive_mind_v01"`,
		`HIVE_DATA_DIR = "` + dataDir + `"`,
		`QDRANT_URL = "http://127.0.0.1:6334"`,
		`QDRANT_API_KEY = "` + apiKey + `"`,
		`OLLAMA_URL = "http://127.0.0.1:11434"`,
		`EMBEDDING_MODEL = "nomic-embed-text"`,
	}, "\n")
}
