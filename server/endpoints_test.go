package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCenterURLPrecedence(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	t.Setenv("HIVE_CENTER_URL", "")

	if got := ResolveCenterURL(nil); got != DefaultCenterURL {
		t.Fatalf("default = %q, want %q", got, DefaultCenterURL)
	}

	if err := os.MkdirAll(filepath.Join(home, ".hive"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".hive", "center-url"), []byte("https://stored.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ResolveCenterURL(nil); got != "https://stored.test" {
		t.Fatalf("stored = %q", got)
	}

	t.Setenv("HIVE_CENTER_URL", "https://env.test")
	if got := ResolveCenterURL(nil); got != "https://env.test" {
		t.Fatalf("env = %q", got)
	}

	if got := ResolveCenterURL([]string{"login", "--center-url", "https://flag.test"}); got != "https://flag.test" {
		t.Fatalf("flag = %q", got)
	}
}

func TestResolveMindURLPrecedence(t *testing.T) {
	setTestHome(t, t.TempDir())
	t.Setenv("HIVE_CENTER_URL", "https://center.test/")
	t.Setenv("HIVE_MIND_URL", "")

	if got := ResolveMindURL(nil); got != "https://center.test/mind" {
		t.Fatalf("derived = %q", got)
	}
	t.Setenv("HIVE_MIND_URL", "https://env-mind.test")
	if got := ResolveMindURL(nil); got != "https://env-mind.test" {
		t.Fatalf("env = %q", got)
	}
	if got := ResolveMindURL([]string{"--mind-url=https://flag-mind.test"}); got != "https://flag-mind.test" {
		t.Fatalf("flag = %q", got)
	}
}

func TestHasLocalConfig(t *testing.T) {
	t.Setenv("HIVE_ID", "")
	t.Setenv("QDRANT_URL", "")
	if hasLocalConfig(nil) {
		t.Fatal("empty environment must be a remote client")
	}
	if !hasLocalConfig([]string{"--config", "/etc/hive.toml"}) {
		t.Fatal("--config must select local mode")
	}
	t.Setenv("QDRANT_URL", "https://127.0.0.1:6334")
	if !hasLocalConfig(nil) {
		t.Fatal("QDRANT_URL must select local mode")
	}
	t.Setenv("QDRANT_URL", "")
	t.Setenv("HIVE_ID", "production")
	if !hasLocalConfig(nil) {
		t.Fatal("HIVE_ID must select local mode")
	}
}
