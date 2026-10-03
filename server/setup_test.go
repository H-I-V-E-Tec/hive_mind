package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupClaudeCodeCreatesConfig(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(origDir) })

	os.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	t.Cleanup(func() { os.Unsetenv("HIVE_MIND_URL") })

	var stderr strings.Builder
	err := RunSetup("claude-code", &stderr)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf("config not created: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	servers := config["mcpServers"].(map[string]any)
	hive := servers["hive_mind"].(map[string]any)
	env := hive["env"].(map[string]any)
	if env["HIVE_MIND_URL"] != "https://mind.hive.test:8443" {
		t.Fatalf("unexpected HIVE_MIND_URL: %v", env["HIVE_MIND_URL"])
	}
}

func TestSetupClaudeAlias(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(origDir) })

	os.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	t.Cleanup(func() { os.Unsetenv("HIVE_MIND_URL") })

	var stderr strings.Builder
	err := RunSetup("claude", &stderr)
	if err != nil {
		t.Fatalf("setup with alias failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); err != nil {
		t.Fatal("config not created via alias")
	}
}

func TestSetupPreservesExistingServers(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(origDir) })

	existing := `{"mcpServers":{"other_tool":{"command":"other","args":[]}}}`
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(existing), 0o644)

	os.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	t.Cleanup(func() { os.Unsetenv("HIVE_MIND_URL") })

	var stderr strings.Builder
	RunSetup("claude-code", &stderr)

	data, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	var config map[string]any
	json.Unmarshal(data, &config)

	servers := config["mcpServers"].(map[string]any)
	if _, ok := servers["other_tool"]; !ok {
		t.Fatal("existing MCP server was removed")
	}
	if _, ok := servers["hive_mind"]; !ok {
		t.Fatal("hive_mind was not added")
	}
}

func TestSetupCodex(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	os.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	t.Cleanup(func() { os.Unsetenv("HIVE_MIND_URL") })

	var stderr strings.Builder
	err := RunSetup("codex", &stderr)
	if err != nil {
		t.Fatalf("setup codex failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(tmpHome, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("codex config not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "[mcp_servers.hive_mind]") {
		t.Fatal("missing TOML section header")
	}
	if !strings.Contains(content, "HIVE_MIND_URL") {
		t.Fatal("missing HIVE_MIND_URL in codex config")
	}
}

func TestSetupRequiresMindURL(t *testing.T) {
	os.Unsetenv("HIVE_MIND_URL")

	var stderr strings.Builder
	err := RunSetup("claude-code", &stderr)
	if err == nil {
		t.Fatal("expected error when HIVE_MIND_URL not set")
	}
}

func TestSetupUnknownAgent(t *testing.T) {
	os.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	t.Cleanup(func() { os.Unsetenv("HIVE_MIND_URL") })

	var stderr strings.Builder
	err := RunSetup("unknown-agent", &stderr)
	if err == nil {
		t.Fatal("expected error for unknown agent")
	}
}
