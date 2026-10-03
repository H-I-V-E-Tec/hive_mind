package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testMindServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "version": "1.2.3"})
	})
	mux.HandleFunc("GET /api/v1/sync-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "idle"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestDoctorRemoteAllGood(t *testing.T) {
	srv := testMindServer(t)

	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Store a valid (non-expired) fake token
	payload := "eyJzdWIiOiJtLTEiLCJuYW1lIjoiQW5hIiwiYXVkIjoibWluZCIsImV4cCI6OTk5OTk5OTk5OSwiaWF0IjoxfQ"
	fakeToken := fmt.Sprintf("eyJhbGciOiJSUzI1NiJ9.%s.sig", payload)
	dir := filepath.Join(tmpHome, ".hive")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "token"), []byte(fakeToken+"\n"), 0o600)

	os.Setenv("HIVE_MIND_URL", srv.URL)
	t.Cleanup(func() { os.Unsetenv("HIVE_MIND_URL") })

	var stderr strings.Builder
	report := RunDoctor(&stderr)

	if !report.OK {
		t.Fatalf("expected OK, got checks: %+v\nstderr: %s", report.Checks, stderr.String())
	}
	if report.Mode != "remote" {
		t.Fatalf("expected remote mode, got %s", report.Mode)
	}
	if len(report.Checks) < 3 {
		t.Fatalf("expected at least 3 checks, got %d", len(report.Checks))
	}
}

func TestDoctorRemoteNoToken(t *testing.T) {
	srv := testMindServer(t)

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	os.Setenv("HIVE_MIND_URL", srv.URL)
	t.Cleanup(func() { os.Unsetenv("HIVE_MIND_URL") })

	var stderr strings.Builder
	report := RunDoctor(&stderr)

	if report.OK {
		t.Fatal("expected failure when no token")
	}
	found := false
	for _, c := range report.Checks {
		if c.Name == "token" && c.Status == "failed" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected failed token check")
	}
}

func TestDoctorRemoteServerDown(t *testing.T) {
	tmpHome := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	dir := filepath.Join(tmpHome, ".hive")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "token"), []byte("fake.token.here\n"), 0o600)

	os.Setenv("HIVE_MIND_URL", "http://127.0.0.1:19999")
	t.Cleanup(func() { os.Unsetenv("HIVE_MIND_URL") })

	var stderr strings.Builder
	report := RunDoctor(&stderr)

	if report.OK {
		t.Fatal("expected failure when server unreachable")
	}
}

func TestDoctorLocalMode(t *testing.T) {
	os.Unsetenv("HIVE_MIND_URL")

	var stderr strings.Builder
	report := RunDoctor(&stderr)

	if report.Mode != "local" {
		t.Fatalf("expected local mode, got %s", report.Mode)
	}
}
