package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type SetupTarget struct {
	Name     string
	Aliases  []string
	Help     string
	ConfigFn func(cmd mcpCommand, mindURL string) error
	// Detect reports whether the agent is installed; `hive setup` without an
	// argument configures only detected agents.
	Detect func() bool
}

// mcpCommand is what an agent runs to start the MCP server over stdio.
type mcpCommand struct {
	Command string
	Args    []string
}

// resolveMCPCommand prefers the HIVE launcher, whose path survives product
// updates; the versioned product binary path would break after `hive update`.
func resolveMCPCommand() (mcpCommand, error) {
	if launcher := strings.TrimSpace(os.Getenv("HIVE_LAUNCHER")); launcher != "" {
		if !filepath.IsAbs(launcher) {
			return mcpCommand{}, errors.New("HIVE_LAUNCHER deve ser um caminho absoluto")
		}
		info, err := os.Stat(launcher)
		if err != nil || !info.Mode().IsRegular() {
			return mcpCommand{}, fmt.Errorf("HIVE_LAUNCHER não aponta para um executável: %s", launcher)
		}
		return mcpCommand{Command: launcher, Args: []string{"mind"}}, nil
	}
	binary, err := os.Executable()
	if err != nil {
		return mcpCommand{}, fmt.Errorf("não foi possível determinar o caminho do binário: %w", err)
	}
	return mcpCommand{Command: binary, Args: []string{}}, nil
}

var setupTargets = []SetupTarget{
	{
		Name:     "claude-code",
		Aliases:  []string{"claude", "cc"},
		Help:     "Registra o MCP no Claude Code para o usuário (claude mcp add-json -s user)",
		ConfigFn: setupClaudeCode,
		Detect:   detectClaudeCode,
	},
	{
		Name:     "claude-desktop",
		Aliases:  []string{"desktop"},
		Help:     "Registra o MCP no Claude Desktop (claude_desktop_config.json)",
		ConfigFn: setupClaudeDesktop,
		Detect:   detectClaudeDesktop,
	},
	{
		Name:     "codex",
		Aliases:  []string{},
		Help:     "Registra o MCP no Codex (~/.codex/config.toml)",
		ConfigFn: setupCodex,
		Detect:   detectCodex,
	},
}

func RunSetup(agent string, stderr io.Writer) error {
	agent = strings.ToLower(strings.TrimSpace(agent))

	cmd, err := resolveMCPCommand()
	if err != nil {
		return err
	}

	mindURL, err := ValidateHiveCenterURL(ResolveMindURL(os.Args[1:]))
	if err != nil {
		return fmt.Errorf("URL do Mind inválida: %w", err)
	}

	if agent == "" || agent == "all" {
		var targets []SetupTarget
		for _, t := range setupTargets {
			if agent == "all" || t.Detect() {
				targets = append(targets, t)
			}
		}
		if len(targets) == 0 {
			fmt.Fprintln(stderr, "Nenhum agente compatível foi encontrado nesta máquina.")
			fmt.Fprintln(stderr)
			return listSetupTargets(stderr)
		}
		fmt.Fprintf(stderr, "Mind: %s\n", mindURL)
		failed := 0
		for _, t := range targets {
			fmt.Fprintf(stderr, "Configurando %s...\n", t.Name)
			if err := t.ConfigFn(cmd, mindURL); err != nil {
				failed++
				fmt.Fprintf(stderr, "  ✗ %s: %v\n", t.Name, err)
			} else {
				fmt.Fprintf(stderr, "  ✓ %s configurado\n", t.Name)
			}
		}
		if failed == len(targets) {
			return errors.New("nenhum agente foi configurado")
		}
		return nil
	}

	for _, t := range setupTargets {
		if t.Name == agent || sliceContainsStr(t.Aliases, agent) {
			if err := t.ConfigFn(cmd, mindURL); err != nil {
				return err
			}
			fmt.Fprintf(stderr, "✓ %s configurado com sucesso\n", t.Name)
			return nil
		}
	}

	fmt.Fprintf(stderr, "Agente '%s' não reconhecido.\n\n", agent)
	return listSetupTargets(stderr)
}

func listSetupTargets(stderr io.Writer) error {
	fmt.Fprintln(stderr, "Uso: hive setup [agente]")
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, "Agentes disponíveis:")
	for _, t := range setupTargets {
		aliases := ""
		if len(t.Aliases) > 0 {
			aliases = fmt.Sprintf(" (aliases: %s)", strings.Join(t.Aliases, ", "))
		}
		fmt.Fprintf(stderr, "  %-16s %s%s\n", t.Name, t.Help, aliases)
	}
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, "  all              Configura todos os agentes acima")
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, "Sem argumento, configura os agentes encontrados nesta máquina.")
	fmt.Fprintln(stderr, "A URL do Mind vem de --mind-url, HIVE_MIND_URL ou do HIVE Center padrão.")
	return errors.New("especifique um agente")
}

// lookPath and claudeCLI are replaced in tests.
var lookPath = exec.LookPath

// claudeCLI runs the Claude Code CLI.
var claudeCLI = func(args ...string) ([]byte, error) {
	path, err := lookPath("claude")
	if err != nil {
		return nil, errors.New("comando 'claude' não encontrado no PATH; instale o Claude Code")
	}
	return exec.Command(path, args...).CombinedOutput()
}

func detectClaudeCode() bool {
	_, err := lookPath("claude")
	return err == nil
}

func detectClaudeDesktop() bool {
	path := claudeDesktopConfigPath()
	if path == "" {
		return false
	}
	info, err := os.Stat(filepath.Dir(path))
	return err == nil && info.IsDir()
}

func detectCodex() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(home, ".codex"))
	return err == nil && info.IsDir()
}

// setupClaudeCode registers the MCP in the user scope, so it works in every
// directory. The CLI is used instead of editing ~/.claude.json, which Claude
// Code rewrites while running.
func setupClaudeCode(cmd mcpCommand, mindURL string) error {
	entry, err := json.Marshal(map[string]any{
		"type":    "stdio",
		"command": cmd.Command,
		"args":    cmd.Args,
		"env": map[string]string{
			"HIVE_MIND_URL": mindURL,
		},
	})
	if err != nil {
		return err
	}
	// Replace an earlier user-scope entry, such as a legacy binary path; a
	// missing entry is not an error.
	_, _ = claudeCLI("mcp", "remove", "hive_mind", "-s", "user")
	if out, err := claudeCLI("mcp", "add-json", "hive_mind", string(entry), "-s", "user"); err != nil {
		if detail := strings.TrimSpace(string(out)); detail != "" {
			return fmt.Errorf("claude mcp add-json falhou: %w: %s", err, detail)
		}
		return fmt.Errorf("claude mcp add-json falhou: %w", err)
	}
	return nil
}

func setupClaudeDesktop(cmd mcpCommand, mindURL string) error {
	path := claudeDesktopConfigPath()
	if path == "" {
		return errors.New("diretório de configuração do Claude Desktop não encontrado para este SO")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data := readJSONFileMap(path)
	servers, _ := data["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["hive_mind"] = map[string]any{
		"command": cmd.Command,
		"args":    cmd.Args,
		"env": map[string]string{
			"HIVE_MIND_URL": mindURL,
		},
	}
	data["mcpServers"] = servers
	return writeJSONFile(path, data, 0o644)
}

func setupCodex(cmd mcpCommand, mindURL string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.toml")
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}

	header := "[mcp_servers.hive_mind]"
	lines := strings.Split(existing, "\n")
	var out []string
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) == header {
			i++
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
				i++
			}
			continue
		}
		out = append(out, lines[i])
		i++
	}

	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	if len(out) > 0 {
		out = append(out, "")
	}
	out = append(out,
		header,
		fmt.Sprintf("command = %q", cmd.Command),
		fmt.Sprintf("args = %s", tomlStringArray(cmd.Args)),
		fmt.Sprintf("[mcp_servers.hive_mind.env]"),
		fmt.Sprintf("HIVE_MIND_URL = %q", mindURL),
	)

	return os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o600)
}

func claudeDesktopConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")
	case "linux":
		config := os.Getenv("XDG_CONFIG_HOME")
		if config == "" {
			config = filepath.Join(home, ".config")
		}
		return filepath.Join(config, "Claude", "claude_desktop_config.json")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, "Claude", "claude_desktop_config.json")
	}
	return ""
}

func readJSONFileMap(path string) map[string]any {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var result map[string]any
	if json.Unmarshal(data, &result) != nil {
		return map[string]any{}
	}
	return result
}

func writeJSONFile(path string, data any, perm os.FileMode) error {
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(encoded, '\n'), perm)
}

func sliceContainsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func tomlStringArray(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
