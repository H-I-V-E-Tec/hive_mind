package server

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestLauncherCommandsAreNotMindCommands(t *testing.T) {
	if cmd := os.Getenv("HIVE_TEST_START_COMMAND"); cmd != "" {
		os.Args = []string{"hive", cmd, "mind"}
		Start("test")
		return
	}
	for _, cmd := range []string{"install", "update", "rollback", "uninstall"} {
		t.Run(cmd, func(t *testing.T) {
			proc := exec.Command(os.Args[0], "-test.run=^TestLauncherCommandsAreNotMindCommands$")
			proc.Env = append(os.Environ(), "HIVE_TEST_START_COMMAND="+cmd)
			out, err := proc.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != ExitUsage {
				t.Fatalf("expected exit %d, got %v\n%s", ExitUsage, err, out)
			}
			if !strings.Contains(string(out), "launcher") || !strings.Contains(string(out), "install-skill") {
				t.Fatalf("missing guidance in output:\n%s", out)
			}
		})
	}
}

func TestInstallSkillStillValidated(t *testing.T) {
	if _, err := splitCLIArgs([]string{"hive", "install-skill", "claude"}); err != nil {
		t.Fatalf("install-skill rejected: %v", err)
	}
}
