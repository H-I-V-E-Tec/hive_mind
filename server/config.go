package server

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	RoleWriter = "writer"
	RoleReader = "reader"
)

var legacyConfigKeys = []string{
	"HIVE_MODE",
	"WATCH_DIRECTORY",
	"QDRANT_COLLECTION",
	"QDRANT_HOST",
	"QDRANT_PORT",
	"OLLAMA_HOST",
}

// Secret controls every fmt representation so configuration dumps cannot
// accidentally expose credential material.
type Secret struct {
	value string
}

func newSecret(value string) Secret { return Secret{value: value} }

func (s Secret) Reveal() string { return s.value }

func (s Secret) IsSet() bool { return s.value != "" }

func (s Secret) String() string { return "[REDACTED]" }

func (s Secret) GoString() string { return "[REDACTED]" }

func (s Secret) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("[REDACTED]"))
}

// Config is the effective Hive Mind configuration. The legacy-named fields at
// the bottom are temporary internal adapters for code replaced by later specs;
// legacy environment variables and flags are never accepted.
type Config struct {
	AuditDirectory      string
	HiveID              string
	DeviceID            string
	WriterApprovalID    string
	Role                string
	CollectionName      string
	ControlCollection   string
	DataDirectory       string
	QdrantURL           string
	QdrantAPIKey        Secret
	QdrantTLSCAFile     string
	QdrantTLSServerName string
	QdrantUseTLS        bool
	QdrantHost          string
	QdrantPort          int
	OllamaURL           string
	EmbeddingModel      string
	MaxClassification   string
	ContextMaxChars     int
	ConfigFile          string

	WatchDirectory      string
	OllamaHost          string
	DebounceDuration    time.Duration
	ExcludeDirs         []string
	IncludeHiddenDirs   []string
	ParserMode          string
	MaxEmbeddingWorkers int
	BatchSize           int
	BatchTimeout        time.Duration
	LogToFile           bool
	SearchMode          string
	ExcludeExtensions   []string
	MaxFileSize         int64
	MaxChunksPerFile    int
	ChunkMaxChars       int
	ChunkOverlapChars   int
	JSONMaxDepth        int
	JSONMaxElements     int
	DeleteGrace         time.Duration
}

func (c Config) IsWriter() bool { return c.Role == RoleWriter }

func (c Config) IsReader() bool { return c.Role == RoleReader }

// Format is intentionally explicit and always redacts QdrantAPIKey.
func (c Config) Format(state fmt.State, _ rune) {
	_, _ = fmt.Fprintf(state,
		"Config{HiveID:%q DeviceID:%q WriterApprovalID:%q Role:%q CollectionName:%q ControlCollection:%q DataDirectory:%q QdrantURL:%q QdrantAPIKey:[REDACTED] OllamaURL:%q EmbeddingModel:%q MaxClassification:%q}",
		c.HiveID, c.DeviceID, c.WriterApprovalID, c.Role, c.CollectionName, c.ControlCollection,
		c.DataDirectory, c.QdrantURL, c.OllamaURL, c.EmbeddingModel, c.MaxClassification,
	)
}

// LoadConfig loads flags, environment, and an explicitly selected TOML file.
// Precedence is flags > environment > file > safe defaults.
func LoadConfig() (Config, error) {
	env := make(map[string]string)
	for _, item := range os.Environ() {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	return LoadConfigFrom(os.Args[1:], env)
}

// LoadConfigFrom is the deterministic entry point used by tests and embedders.
func LoadConfigFrom(args []string, env map[string]string) (Config, error) {
	for _, key := range legacyConfigKeys {
		if _, exists := env[key]; exists {
			return Config{}, fmt.Errorf("legacy configuration %s is not supported", key)
		}
	}

	flagValues, configPath, err := parseConfigFlags(args)
	if err != nil {
		return Config{}, err
	}

	values := map[string]string{"HIVE_MAX_CLASSIFICATION": "internal"}
	fileContainsSecret := false
	if configPath != "" {
		fileValues, err := loadExplicitTOML(configPath)
		if err != nil {
			return Config{}, err
		}
		for key, value := range fileValues {
			values[key] = value
		}
		_, fileContainsSecret = fileValues["QDRANT_API_KEY"]
	}
	for key := range allowedConfigKeys {
		if value, exists := env[key]; exists {
			values[key] = value
		}
	}
	for key, value := range flagValues {
		values[key] = value
	}

	cfg, err := buildConfig(values, configPath)
	if err != nil {
		return Config{}, err
	}
	if configPath != "" && fileContainsSecret {
		if err := validateSecretConfigFile(configPath, cfg.DataDirectory); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

var allowedConfigKeys = map[string]struct{}{
	"HIVE_AUDIT_DIR": {},
	"HIVE_ID":        {}, "HIVE_DEVICE_ID": {}, "HIVE_WRITER_APPROVAL_ID": {}, "HIVE_ROLE": {},
	"HIVE_COLLECTION": {}, "HIVE_DATA_DIR": {},
	"HIVE_MAX_CLASSIFICATION": {}, "QDRANT_URL": {},
	"HIVE_CONTEXT_MAX_CHARS": {},
	"QDRANT_API_KEY":         {}, "QDRANT_TLS_CA_FILE": {},
	"QDRANT_TLS_SERVER_NAME": {}, "OLLAMA_URL": {},
	"EMBEDDING_MODEL":     {},
	"HIVE_MAX_FILE_BYTES": {}, "HIVE_MAX_CHUNKS_PER_FILE": {},
	"HIVE_CHUNK_MAX_CHARS": {}, "HIVE_CHUNK_OVERLAP_CHARS": {},
	"HIVE_JSON_MAX_DEPTH": {}, "HIVE_JSON_MAX_ELEMENTS": {},
	"HIVE_MAX_EMBEDDING_WORKERS": {}, "HIVE_DELETE_GRACE_HOURS": {},
}

var configFlagKeys = map[string]string{
	"--hive-id":                "HIVE_ID",
	"--device-id":              "HIVE_DEVICE_ID",
	"--writer-approval-id":     "HIVE_WRITER_APPROVAL_ID",
	"--role":                   "HIVE_ROLE",
	"--collection":             "HIVE_COLLECTION",
	"--data-dir":               "HIVE_DATA_DIR",
	"--max-classification":     "HIVE_MAX_CLASSIFICATION",
	"--context-max-chars":      "HIVE_CONTEXT_MAX_CHARS",
	"--qdrant-url":             "QDRANT_URL",
	"--qdrant-tls-ca-file":     "QDRANT_TLS_CA_FILE",
	"--qdrant-tls-server-name": "QDRANT_TLS_SERVER_NAME",
	"--ollama-url":             "OLLAMA_URL",
	"--embedding-model":        "EMBEDDING_MODEL",
	"--max-file-bytes":         "HIVE_MAX_FILE_BYTES",
	"--max-chunks-per-file":    "HIVE_MAX_CHUNKS_PER_FILE",
	"--chunk-max-chars":        "HIVE_CHUNK_MAX_CHARS",
	"--chunk-overlap-chars":    "HIVE_CHUNK_OVERLAP_CHARS",
	"--json-max-depth":         "HIVE_JSON_MAX_DEPTH",
	"--json-max-elements":      "HIVE_JSON_MAX_ELEMENTS",
	"--max-embedding-workers":  "HIVE_MAX_EMBEDDING_WORKERS",
	"--delete-grace-hours":     "HIVE_DELETE_GRACE_HOURS",
}

func parseConfigFlags(args []string) (map[string]string, string, error) {
	values := make(map[string]string)
	configPath := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--prune" || arg == "--yes" || isOperationalFlag(arg) {
			continue
		}
		if arg == "--qdrant-api-key" || strings.HasPrefix(arg, "--qdrant-api-key=") {
			return nil, "", errors.New("QDRANT_API_KEY is not accepted as a process argument; use the environment or protected config file")
		}
		name, inlineValue, hasInlineValue := strings.Cut(arg, "=")
		if name == "--config" {
			value, consumed, err := flagValue(name, inlineValue, hasInlineValue, args, i)
			if err != nil {
				return nil, "", err
			}
			if consumed {
				i++
			}
			if configPath != "" {
				return nil, "", errors.New("--config may be provided only once")
			}
			configPath = value
			continue
		}
		if key, exists := configFlagKeys[name]; exists {
			value, consumed, err := flagValue(name, inlineValue, hasInlineValue, args, i)
			if err != nil {
				return nil, "", err
			}
			if consumed {
				i++
			}
			values[key] = value
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return nil, "", errors.New("unsupported configuration flag")
		}
	}
	return values, configPath, nil
}

func isOperationalFlag(arg string) bool {
	name, _, hasValue := strings.Cut(arg, "=")
	if !hasValue {
		return false
	}
	switch name {
	case "--document-type", "--tag", "--classification", "--scope-status", "--limit":
		return true
	default:
		return false
	}
}

func flagValue(name, inlineValue string, hasInlineValue bool, args []string, index int) (string, bool, error) {
	if hasInlineValue {
		if inlineValue == "" {
			return "", false, fmt.Errorf("%s requires a value", name)
		}
		return inlineValue, false, nil
	}
	if index+1 >= len(args) || strings.HasPrefix(args[index+1], "-") {
		return "", false, fmt.Errorf("%s requires a value", name)
	}
	return args[index+1], true, nil
}

func loadExplicitTOML(path string) (map[string]string, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("explicit configuration must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open the explicit configuration file")
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(stripTOMLComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			return nil, fmt.Errorf("configuration file line %d: TOML sections are not supported", lineNumber)
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("configuration file line %d is invalid", lineNumber)
		}
		key := strings.TrimSpace(parts[0])
		if _, allowed := allowedConfigKeys[key]; !allowed {
			return nil, fmt.Errorf("configuration file line %d contains an unsupported key", lineNumber)
		}
		if _, duplicate := values[key]; duplicate {
			return nil, fmt.Errorf("configuration file line %d repeats key %s", lineNumber, key)
		}
		value, err := parseTOMLScalar(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("configuration file line %d has an invalid value", lineNumber)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, errors.New("cannot read the explicit configuration file")
	}
	return values, nil
}

func stripTOMLComment(line string) string {
	var quote rune
	escaped := false
	for index, char := range line {
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && char == '\\' {
			escaped = true
			continue
		}
		if char == '\'' || char == '"' {
			if quote == 0 {
				quote = char
			} else if quote == char {
				quote = 0
			}
			continue
		}
		if char == '#' && quote == 0 {
			return line[:index]
		}
	}
	return line
}

func parseTOMLScalar(raw string) (string, error) {
	if raw == "" {
		return "", errors.New("empty value")
	}
	if strings.HasPrefix(raw, "\"") {
		value, err := strconv.Unquote(raw)
		if err != nil {
			return "", err
		}
		return value, nil
	}
	if strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'") && len(raw) >= 2 {
		return raw[1 : len(raw)-1], nil
	}
	return "", errors.New("configuration values must be TOML strings")
}

func buildConfig(values map[string]string, configPath string) (Config, error) {
	required := []string{
		"HIVE_ID", "HIVE_DEVICE_ID", "HIVE_ROLE", "HIVE_COLLECTION",
		"QDRANT_URL", "QDRANT_API_KEY", "OLLAMA_URL", "EMBEDDING_MODEL",
	}
	for _, key := range required {
		if strings.TrimSpace(values[key]) == "" {
			return Config{}, fmt.Errorf("missing required configuration %s", key)
		}
	}

	hiveID := strings.TrimSpace(values["HIVE_ID"])
	deviceID := strings.TrimSpace(values["HIVE_DEVICE_ID"])
	collection := strings.TrimSpace(values["HIVE_COLLECTION"])
	if !validIdentifier(hiveID, 64) {
		return Config{}, errors.New("HIVE_ID must match [a-z0-9][a-z0-9_-]{0,63}")
	}
	if !validIdentifier(deviceID, 64) {
		return Config{}, errors.New("HIVE_DEVICE_ID must match [a-z0-9][a-z0-9_-]{0,63}")
	}
	if !validIdentifier(collection, 55) {
		return Config{}, errors.New("HIVE_COLLECTION must match [a-z0-9][a-z0-9_-]{0,54}")
	}

	role := strings.ToLower(strings.TrimSpace(values["HIVE_ROLE"]))
	if role != RoleWriter && role != RoleReader {
		return Config{}, errors.New("HIVE_ROLE must be writer or reader")
	}
	writerApprovalID := strings.TrimSpace(values["HIVE_WRITER_APPROVAL_ID"])
	if role == RoleWriter && writerApprovalID == "" {
		return Config{}, errors.New("missing required configuration HIVE_WRITER_APPROVAL_ID for writer")
	}
	if writerApprovalID != "" && !validIdentifier(writerApprovalID, 64) {
		return Config{}, errors.New("HIVE_WRITER_APPROVAL_ID must match [a-z0-9][a-z0-9_-]{0,63}")
	}

	qdrantURL, qdrantHost, qdrantPort, qdrantTLS, err := validateServiceURL(values["QDRANT_URL"], "QDRANT_URL", false)
	if err != nil {
		return Config{}, err
	}
	if !qdrantTLS && !isLoopbackHost(qdrantHost) {
		return Config{}, errors.New("QDRANT_URL outside loopback must use https")
	}

	ollamaURL, ollamaHost, _, _, err := validateServiceURL(values["OLLAMA_URL"], "OLLAMA_URL", true)
	if err != nil {
		return Config{}, err
	}
	if !isLoopbackHost(ollamaHost) {
		return Config{}, errors.New("OLLAMA_URL must use a loopback host in v0.1")
	}

	apiKey := values["QDRANT_API_KEY"]
	if strings.TrimSpace(apiKey) != apiKey || containsControl(apiKey) || len(apiKey) > 8192 {
		return Config{}, errors.New("QDRANT_API_KEY has an invalid format")
	}
	embeddingModel := strings.TrimSpace(values["EMBEDDING_MODEL"])
	if containsControl(embeddingModel) || len(embeddingModel) > 200 {
		return Config{}, errors.New("EMBEDDING_MODEL has an invalid format")
	}

	classification := strings.ToLower(strings.TrimSpace(values["HIVE_MAX_CLASSIFICATION"]))
	if classification != "internal" && classification != "restricted" {
		return Config{}, errors.New("HIVE_MAX_CLASSIFICATION must be internal or restricted")
	}

	dataDirectory := ""
	if rawDir := strings.TrimSpace(values["HIVE_DATA_DIR"]); rawDir != "" {
		dataDirectory, err = validateDataDirectory(rawDir)
		if err != nil {
			return Config{}, err
		}
	} else if role == RoleWriter {
		return Config{}, errors.New("missing required configuration HIVE_DATA_DIR for writer")
	}

	caFile := strings.TrimSpace(values["QDRANT_TLS_CA_FILE"])
	serverName := strings.TrimSpace(values["QDRANT_TLS_SERVER_NAME"])
	if !qdrantTLS && (caFile != "" || serverName != "") {
		return Config{}, errors.New("QDRANT TLS options require an https QDRANT_URL")
	}
	if caFile != "" {
		caFile, err = validateRegularReadableFile(caFile, "QDRANT_TLS_CA_FILE")
		if err != nil {
			return Config{}, err
		}
	}
	if serverName != "" && !validServerName(serverName) {
		return Config{}, errors.New("QDRANT_TLS_SERVER_NAME is invalid")
	}

	canonicalConfigPath := ""
	if configPath != "" {
		canonicalConfigPath, _ = filepath.Abs(configPath)
	}

	maxFileBytes, err := boundedInt(values, "HIVE_MAX_FILE_BYTES", 5*1024*1024, 1, 50*1024*1024)
	if err != nil {
		return Config{}, err
	}
	maxChunks, err := boundedInt(values, "HIVE_MAX_CHUNKS_PER_FILE", 1000, 1, 5000)
	if err != nil {
		return Config{}, err
	}
	chunkMax, err := boundedInt(values, "HIVE_CHUNK_MAX_CHARS", 2000, 1, 8000)
	if err != nil {
		return Config{}, err
	}
	overlap, err := boundedInt(values, "HIVE_CHUNK_OVERLAP_CHARS", 200, 0, chunkMax/2)
	if err != nil {
		return Config{}, err
	}
	jsonDepth, err := boundedInt(values, "HIVE_JSON_MAX_DEPTH", 64, 1, 64)
	if err != nil {
		return Config{}, err
	}
	jsonElements, err := boundedInt(values, "HIVE_JSON_MAX_ELEMENTS", 100000, 1, 100000)
	if err != nil {
		return Config{}, err
	}
	workers, err := boundedInt(values, "HIVE_MAX_EMBEDDING_WORKERS", 2, 1, 16)
	if err != nil {
		return Config{}, err
	}
	graceHours, err := boundedInt(values, "HIVE_DELETE_GRACE_HOURS", 24, 1, 24)
	if err != nil {
		return Config{}, err
	}
	contextMaxChars, err := boundedInt(values, "HIVE_CONTEXT_MAX_CHARS", 12000, 1000, 50000)
	if err != nil {
		return Config{}, err
	}

	return Config{
		AuditDirectory: strings.TrimSpace(values["HIVE_AUDIT_DIR"]),
		HiveID:         hiveID, DeviceID: deviceID, WriterApprovalID: writerApprovalID, Role: role,
		CollectionName: collection, ControlCollection: collection + "__control",
		DataDirectory: dataDirectory, QdrantURL: qdrantURL,
		QdrantAPIKey: newSecret(apiKey), QdrantTLSCAFile: caFile,
		QdrantTLSServerName: serverName, QdrantUseTLS: qdrantTLS,
		QdrantHost: qdrantHost, QdrantPort: qdrantPort,
		OllamaURL: ollamaURL, EmbeddingModel: embeddingModel,
		MaxClassification: classification, ContextMaxChars: contextMaxChars, ConfigFile: canonicalConfigPath,

		WatchDirectory: dataDirectory, OllamaHost: ollamaURL,
		DebounceDuration: 800 * time.Millisecond, ParserMode: "doc",
		MaxEmbeddingWorkers: workers, BatchSize: 100,
		BatchTimeout: 200 * time.Millisecond, SearchMode: "dense",
		MaxFileSize: int64(maxFileBytes), MaxChunksPerFile: maxChunks,
		ChunkMaxChars: chunkMax, ChunkOverlapChars: overlap,
		JSONMaxDepth: jsonDepth, JSONMaxElements: jsonElements,
		DeleteGrace: time.Duration(graceHours) * time.Hour,
	}, nil
}

func boundedInt(values map[string]string, key string, defaultValue, minValue, maxValue int) (int, error) {
	raw := strings.TrimSpace(values[key])
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue || value > maxValue {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", key, minValue, maxValue)
	}
	return value, nil
}

func validateServiceURL(raw, key string, requireHTTP bool) (string, string, int, bool, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return "", "", 0, false, fmt.Errorf("%s must be an absolute URL with scheme", key)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", 0, false, fmt.Errorf("%s must use http or https", key)
	}
	if requireHTTP && parsed.Scheme != "http" {
		return "", "", 0, false, fmt.Errorf("%s must use http for local Ollama", key)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", "", 0, false, fmt.Errorf("%s must not contain credentials, path, query, or fragment", key)
	}
	host := parsed.Hostname()
	port := 6334
	if key == "OLLAMA_URL" {
		port = 11434
	}
	if parsed.Port() != "" {
		parsedPort, err := strconv.Atoi(parsed.Port())
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return "", "", 0, false, fmt.Errorf("%s contains an invalid port", key)
		}
		port = parsedPort
	}
	parsed.Path = ""
	return strings.TrimSuffix(parsed.String(), "/"), host, port, parsed.Scheme == "https", nil
}

func validateDataDirectory(raw string) (string, error) {
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", errors.New("HIVE_DATA_DIR cannot be resolved")
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", errors.New("HIVE_DATA_DIR does not exist or cannot be resolved")
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return "", errors.New("HIVE_DATA_DIR must be an existing directory")
	}
	dir, err := os.Open(real)
	if err != nil {
		return "", errors.New("HIVE_DATA_DIR is not readable")
	}
	_ = dir.Close()
	return filepath.Clean(real), nil
}

func validateRegularReadableFile(raw, key string) (string, error) {
	abs, err := filepath.Abs(raw)
	if err != nil {
		return "", fmt.Errorf("%s cannot be resolved", key)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%s does not exist or cannot be resolved", key)
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s must be a regular file", key)
	}
	file, err := os.Open(real)
	if err != nil {
		return "", fmt.Errorf("%s is not readable", key)
	}
	_ = file.Close()
	return filepath.Clean(real), nil
}

func validateSecretConfigFile(path, dataDirectory string) error {
	info, err := os.Stat(path)
	if err != nil {
		return errors.New("cannot inspect the explicit configuration file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return errors.New("configuration file containing QDRANT_API_KEY must not be accessible by group or others")
	}
	if dataDirectory == "" {
		return nil
	}
	configReal, err := filepath.EvalSymlinks(path)
	if err != nil {
		return errors.New("cannot resolve the explicit configuration file")
	}
	rel, err := filepath.Rel(dataDirectory, configReal)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("configuration file containing QDRANT_API_KEY must be outside HIVE_DATA_DIR")
	}
	return nil
}

// QdrantTLSConfig constructs verified TLS settings without a skip-verification mode.
func (c Config) QdrantTLSConfig() (*tls.Config, error) {
	if !c.QdrantUseTLS {
		return nil, nil
	}
	serverName := c.QdrantTLSServerName
	if serverName == "" {
		serverName = c.QdrantHost
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	if c.QdrantTLSCAFile == "" {
		return tlsConfig, nil
	}
	pem, err := os.ReadFile(c.QdrantTLSCAFile)
	if err != nil {
		return nil, errors.New("cannot read QDRANT_TLS_CA_FILE")
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, errors.New("QDRANT_TLS_CA_FILE contains no valid certificate")
	}
	tlsConfig.RootCAs = roots
	return tlsConfig, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validServerName(value string) bool {
	if containsControl(value) || strings.ContainsAny(value, "/:@ ") {
		return false
	}
	if ip := net.ParseIP(value); ip != nil {
		return true
	}
	return strings.Contains(value, ".") || validIdentifier(value, 64)
}

func containsControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func validIdentifier(value string, maxLength int) bool {
	if value == "" || len(value) > maxLength || !isLowerAlphaNumeric(value[0]) {
		return false
	}
	for _, char := range value[1:] {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func isLowerAlphaNumeric(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= '0' && char <= '9'
}
