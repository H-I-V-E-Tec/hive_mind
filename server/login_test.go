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

func testCenterServer(t *testing.T, wantUser, wantPass string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/token" || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Audience string `json:"audience"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_request"})
			return
		}
		if body.Username != wantUser || body.Password != wantPass {
			w.WriteHeader(401)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_credentials"})
			return
		}
		if body.Audience != "mind" {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_audience"})
			return
		}
		// Return a fake JWT (three dot-separated base64 segments).
		header := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9"
		// {"sub":"m-1","name":"Ana","aud":"mind","exp":9999999999,"iat":1}
		payload := "eyJzdWIiOiJtLTEiLCJuYW1lIjoiQW5hIiwiYXVkIjoibWluZCIsImV4cCI6OTk5OTk5OTk5OSwiaWF0IjoxfQ"
		sig := "fake-sig"
		token := fmt.Sprintf("%s.%s.%s", header, payload, sig)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": token,
			"token_type":   "bearer",
			"expires_in":   900,
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunLoginSuccess(t *testing.T) {
	srv := testCenterServer(t, "admin", "secret")

	// Override token dir to temp dir.
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	stdin := strings.NewReader("admin\nsecret\n")
	var stderr strings.Builder

	err := RunLogin(srv.URL, stdin, &stderr)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	tokenPath := filepath.Join(tmpHome, ".hive", "token")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("token file not found: %v", err)
	}
	token := strings.TrimSpace(string(data))
	if !strings.Contains(token, ".") || len(token) < 10 {
		t.Errorf("token looks invalid: %q", token)
	}

	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file permissions = %o, want 600", perm)
	}

	dirInfo, err := os.Stat(filepath.Join(tmpHome, ".hive"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf(".hive dir permissions = %o, want 700", perm)
	}

	output := stderr.String()
	if !strings.Contains(output, "Authenticated as Ana") {
		t.Errorf("output missing name: %q", output)
	}
}

func TestRunLoginBadCredentials(t *testing.T) {
	srv := testCenterServer(t, "admin", "secret")

	origHome := os.Getenv("HOME")
	os.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	stdin := strings.NewReader("admin\nwrong\n")
	var stderr strings.Builder

	err := RunLogin(srv.URL, stdin, &stderr)
	if err == nil {
		t.Fatal("expected error for bad credentials")
	}
	if !strings.Contains(err.Error(), "invalid_credentials") {
		t.Errorf("error = %v, want invalid_credentials", err)
	}
}

func TestRunLoginCheckExpired(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Store a token with exp in the past.
	// {"sub":"m-1","name":"Ana","aud":"mind","exp":1,"iat":0}
	payload := "eyJzdWIiOiJtLTEiLCJuYW1lIjoiQW5hIiwiYXVkIjoibWluZCIsImV4cCI6MSwiaWF0IjowfQ"
	fakeToken := fmt.Sprintf("eyJhbGciOiJSUzI1NiJ9.%s.sig", payload)

	dir := filepath.Join(tmpHome, ".hive")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "token"), []byte(fakeToken+"\n"), 0o600)

	var stderr strings.Builder
	err := RunLoginCheck(&stderr)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %v, want expired", err)
	}
}

func TestRunLoginCheckValid(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// {"sub":"m-1","name":"Ana","aud":"mind","exp":9999999999,"iat":1}
	payload := "eyJzdWIiOiJtLTEiLCJuYW1lIjoiQW5hIiwiYXVkIjoibWluZCIsImV4cCI6OTk5OTk5OTk5OSwiaWF0IjoxfQ"
	fakeToken := fmt.Sprintf("eyJhbGciOiJSUzI1NiJ9.%s.sig", payload)

	dir := filepath.Join(tmpHome, ".hive")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, "token"), []byte(fakeToken+"\n"), 0o600)

	var stderr strings.Builder
	err := RunLoginCheck(&stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := stderr.String()
	if !strings.Contains(output, "Ana") {
		t.Errorf("output missing name: %q", output)
	}
}

func TestRunLoginEmptyUsername(t *testing.T) {
	stdin := strings.NewReader("\n")
	var stderr strings.Builder
	err := RunLogin("http://localhost:9999", stdin, &stderr)
	if err == nil {
		t.Fatal("expected error for empty username")
	}
}

func TestValidateHiveCenterURLRejectsInsecure(t *testing.T) {
	_, err := ValidateHiveCenterURL("http://remote.example.com")
	if err == nil {
		t.Fatal("expected error for non-loopback HTTP")
	}
}

func TestValidateHiveCenterURLAcceptsLoopbackHTTP(t *testing.T) {
	u, err := ValidateHiveCenterURL("http://localhost:8080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != "http://localhost:8080" {
		t.Errorf("url = %q", u)
	}
}

func TestValidateHiveCenterURLAcceptsHTTPS(t *testing.T) {
	u, err := ValidateHiveCenterURL("https://center.hive.example")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != "https://center.hive.example" {
		t.Errorf("url = %q", u)
	}
}
