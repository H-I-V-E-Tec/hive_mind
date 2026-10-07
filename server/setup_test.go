package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Tests must never reach the real Claude Code CLI: it would rewrite the
// developer's ~/.claude.json with the test binary.
func init() {
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	claudeCLI = func(...string) ([]byte, error) {
		return nil, errors.New("claude CLI is disabled in tests; use stubClaudeCLI")
	}
}

// stubClaudeCLI records claude CLI calls and returns the registered entry.
func stubClaudeCLI(t *testing.T, fail bool) (calls *[][]string, entry func() map[string]any) {
	t.Helper()
	var recorded [][]string
	orig := claudeCLI
	claudeCLI = func(args ...string) ([]byte, error) {
		recorded = append(recorded, args)
		if fail && len(args) > 1 && args[1] == "add-json" {
			return []byte("boom"), errors.New("exit status 1")
		}
		return nil, nil
	}
	t.Cleanup(func() { claudeCLI = orig })
	return &recorded, func() map[string]any {
		t.Helper()
		for _, call := range recorded {
			if len(call) == 6 && call[1] == "add-json" {
				var e map[string]any
				if err := json.Unmarshal([]byte(call[3]), &e); err != nil {
					t.Fatalf("invalid entry JSON: %v", err)
				}
				return e
			}
		}
		t.Fatalf("add-json not called: %v", recorded)
		return nil
	}
}

func TestSetupClaudeCodeRegistersUserScope(t *testing.T) {
	t.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	calls, entry := stubClaudeCLI(t, false)

	var stderr strings.Builder
	if err := RunSetup("claude-code", &stderr); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	want := [][]string{
		{"mcp", "remove", "hive_mind", "-s", "user"},
		{"mcp", "add-json", "hive_mind", (*calls)[1][3], "-s", "user"},
	}
	if fmt.Sprint(*calls) != fmt.Sprint(want) {
		t.Fatalf("calls = %v, want %v", *calls, want)
	}
	e := entry()
	if e["type"] != "stdio" {
		t.Fatalf("type = %v, want stdio", e["type"])
	}
	env := e["env"].(map[string]any)
	if env["HIVE_MIND_URL"] != "https://mind.hive.test:8443" {
		t.Fatalf("unexpected HIVE_MIND_URL: %v", env["HIVE_MIND_URL"])
	}
}

func TestSetupClaudeAlias(t *testing.T) {
	t.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	_, entry := stubClaudeCLI(t, false)

	var stderr strings.Builder
	if err := RunSetup("claude", &stderr); err != nil {
		t.Fatalf("setup with alias failed: %v", err)
	}
	entry()
}

func TestSetupClaudeCodeReportsCLIFailure(t *testing.T) {
	t.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	stubClaudeCLI(t, true)

	var stderr strings.Builder
	err := RunSetup("claude-code", &stderr)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected CLI failure with output, got %v", err)
	}
}

func TestSetupCodex(t *testing.T) {
	tmpHome := t.TempDir()
	setTestHome(t, tmpHome)

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

// Rerunning setup over a prior hive_mind entry (including an orphaned env
// subtable and a user-managed tools subtable) must leave exactly one server
// table and one env table — never a duplicate [mcp_servers.hive_mind.env],
// which TOML rejects as a double declaration — while preserving subtables the
// setup does not manage.
func TestSetupCodexReplacesWithoutDuplicatingEnv(t *testing.T) {
	tmpHome := t.TempDir()
	setTestHome(t, tmpHome)

	os.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	t.Cleanup(func() { os.Unsetenv("HIVE_MIND_URL") })

	dir := filepath.Join(tmpHome, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	prior := strings.Join([]string{
		"model = \"gpt\"",
		"",
		"[mcp_servers.hive_mind.tools.hive_search]",
		"approval_mode = \"approve\"",
		"",
		"[mcp_servers.hive_mind.env]",
		"HIVE_MIND_URL = \"https://old.example/mind\"",
		"",
		"[mcp_servers.hive_mind]",
		"command = \"/old/hive\"",
		"args = [\"mind\"]",
		"[mcp_servers.hive_mind.env]",
		"HIVE_MIND_URL = \"https://old.example/mind\"",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(prior), 0o600); err != nil {
		t.Fatal(err)
	}

	var stderr strings.Builder
	if err := RunSetup("codex", &stderr); err != nil {
		t.Fatalf("setup codex failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("codex config not created: %v", err)
	}
	content := string(data)

	if n := strings.Count(content, "[mcp_servers.hive_mind.env]"); n != 1 {
		t.Fatalf("expected exactly one env table, got %d:\n%s", n, content)
	}
	if n := strings.Count(content, "[mcp_servers.hive_mind]"); n != 1 {
		t.Fatalf("expected exactly one server table, got %d:\n%s", n, content)
	}
	if !strings.Contains(content, "[mcp_servers.hive_mind.tools.hive_search]") {
		t.Fatalf("user-managed tools subtable was dropped:\n%s", content)
	}
	if strings.Contains(content, "https://old.example/mind") {
		t.Fatalf("stale Mind URL survived the rewrite:\n%s", content)
	}
	if !strings.Contains(content, "https://mind.hive.test:8443") {
		t.Fatalf("new Mind URL missing:\n%s", content)
	}
}

func TestSetupDefaultsMindURL(t *testing.T) {
	setTestHome(t, t.TempDir())
	t.Setenv("HIVE_MIND_URL", "")
	t.Setenv("HIVE_CENTER_URL", "")
	_, entry := stubClaudeCLI(t, false)

	var stderr strings.Builder
	if err := RunSetup("claude-code", &stderr); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	env := entry()["env"].(map[string]any)
	if want := DefaultCenterURL + "/mind"; env["HIVE_MIND_URL"] != want {
		t.Fatalf("HIVE_MIND_URL = %v, want %s", env["HIVE_MIND_URL"], want)
	}
}

func TestSetupRejectsInvalidMindURL(t *testing.T) {
	t.Setenv("HIVE_MIND_URL", "http://mind.example.com")
	calls, _ := stubClaudeCLI(t, false)

	var stderr strings.Builder
	if err := RunSetup("claude-code", &stderr); err == nil {
		t.Fatal("expected rejection of a non-loopback http Mind URL")
	}
	if len(*calls) != 0 {
		t.Fatalf("claude CLI must not run on invalid URL: %v", *calls)
	}
}

func TestSetupWithoutAgentConfiguresDetected(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	t.Setenv("HIVE_MIND_URL", "https://mind.hive.test")
	if err := os.Mkdir(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	calls, _ := stubClaudeCLI(t, false) // claude is not on PATH in tests

	var stderr strings.Builder
	if err := RunSetup("", &stderr); err != nil {
		t.Fatalf("setup failed: %v\n%s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err != nil {
		t.Fatalf("detected codex was not configured: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("undetected claude-code was configured: %v", *calls)
	}
	if desktop := claudeDesktopConfigPath(); desktop != "" {
		if _, err := os.Stat(desktop); err == nil {
			t.Fatal("undetected Claude Desktop was configured")
		}
	}
}

func TestSetupWithoutAgentFailsWhenNoneDetected(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	t.Setenv("HIVE_MIND_URL", "https://mind.hive.test")
	stubClaudeCLI(t, false)

	var stderr strings.Builder
	if err := RunSetup("", &stderr); err == nil {
		t.Fatal("expected error when no agent is detected")
	}
}

func TestSetupWithoutAgentUsesClaudeWhenOnPath(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	t.Setenv("HIVE_MIND_URL", "https://mind.hive.test")
	_, entry := stubClaudeCLI(t, false)
	origLookPath := lookPath
	lookPath = func(string) (string, error) { return "/usr/bin/claude", nil }
	t.Cleanup(func() { lookPath = origLookPath })

	var stderr strings.Builder
	if err := RunSetup("", &stderr); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	entry()
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

func writeFakeLauncher(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hive")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSetupClaudeCodeUsesLauncherPath(t *testing.T) {
	_, entry := stubClaudeCLI(t, false)
	t.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	launcher := writeFakeLauncher(t)
	t.Setenv("HIVE_LAUNCHER", launcher)

	var stderr strings.Builder
	if err := RunSetup("claude-code", &stderr); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	hive := entry()
	if hive["command"] != launcher {
		t.Fatalf("command = %v, want launcher %s", hive["command"], launcher)
	}
	args, _ := hive["args"].([]any)
	if len(args) != 1 || args[0] != "mind" {
		t.Fatalf("args = %v, want [mind]", hive["args"])
	}
}

func TestSetupWithoutLauncherUsesOwnBinary(t *testing.T) {
	_, entry := stubClaudeCLI(t, false)
	t.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	t.Setenv("HIVE_LAUNCHER", "")

	var stderr strings.Builder
	if err := RunSetup("claude-code", &stderr); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	hive := entry()
	self, _ := os.Executable()
	if hive["command"] != self {
		t.Fatalf("command = %v, want %s", hive["command"], self)
	}
	if args, _ := hive["args"].([]any); len(args) != 0 {
		t.Fatalf("args = %v, want []", args)
	}
}

func TestSetupCodexUsesLauncherPath(t *testing.T) {
	setTestHome(t, t.TempDir())
	t.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	launcher := writeFakeLauncher(t)
	t.Setenv("HIVE_LAUNCHER", launcher)

	var stderr strings.Builder
	if err := RunSetup("codex", &stderr); err != nil {
		t.Fatalf("setup codex failed: %v", err)
	}
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, fmt.Sprintf("command = %q", launcher)) || !strings.Contains(content, `args = ["mind"]`) {
		t.Fatalf("codex config does not use the launcher:\n%s", content)
	}
}

func TestSetupRejectsInvalidLauncher(t *testing.T) {
	t.Setenv("HIVE_MIND_URL", "https://mind.hive.test:8443")
	for _, value := range []string{"relative/hive", filepath.Join(t.TempDir(), "missing")} {
		t.Setenv("HIVE_LAUNCHER", value)
		var stderr strings.Builder
		if err := RunSetup("claude-code", &stderr); err == nil || !strings.Contains(err.Error(), "HIVE_LAUNCHER") {
			t.Fatalf("HIVE_LAUNCHER=%q: expected rejection, got %v", value, err)
		}
	}
}
