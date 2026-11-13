package server

import (
	"context"
	"encoding/base64"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestSensitiveEndpointsRejectRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Setenv("HIVE_HOME", t.TempDir())
			t.Setenv("HIVE_TOKEN", "synthetic-token")
			var leaked atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				leaked.Add(1)
				w.Write([]byte(`{"status":"ok"}`))
			}))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, status)
			}))
			defer source.Close()
			if err := RunLogin(source.URL, strings.NewReader("user\npassword\n"), io.Discard); err == nil {
				t.Fatal("redirected login succeeded")
			}
			if err := NewRemoteClient(source.URL).postJSON(context.Background(), "/api/v1/search", map[string]string{"query": "private"}, nil); err == nil {
				t.Fatal("redirected API request succeeded")
			}
			if err := NewJWKSClient(source.URL).ensureFresh(context.Background()); err == nil {
				t.Fatal("redirected JWKS succeeded")
			}
			report := DoctorReport{OK: true}
			runRemoteDoctor(&report, source.URL, io.Discard)
			if report.OK || leaked.Load() != 0 {
				t.Fatalf("redirect followed: doctor OK=%v, target requests=%d", report.OK, leaked.Load())
			}
		})
	}
}

func TestCenterURLRejectsAmbiguousComponents(t *testing.T) {
	for _, raw := range []string{"https://user:password@example.com", "https://example.com?token=secret", "https://example.com?", "https://example.com#fragment", "https://:443"} {
		if _, err := ValidateHiveCenterURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestJWKSRejectsIncompatibleAndDuplicateKeys(t *testing.T) {
	key := testRSAKey(t)
	valid := testJWKS(t, "key", &key.PublicKey)
	for _, raw := range []string{
		strings.Replace(valid, `"RS256"`, `"RS512"`, 1),
		strings.Replace(valid, `"sig"`, `"enc"`, 1),
		strings.Replace(valid, `]}`, ","+strings.TrimSuffix(strings.TrimPrefix(valid, `{"keys":[`), `]}`)+`]}`, 1),
	} {
		srv := testJWKSServer(t, raw)
		if err := NewJWKSClient(srv.URL).ensureFresh(context.Background()); err == nil {
			t.Fatal("invalid signing key set accepted")
		}
	}
}

func TestRSAKeyRejectsWeakParameters(t *testing.T) {
	encode := func(n *big.Int) string { return base64.RawURLEncoding.EncodeToString(n.Bytes()) }
	validN := testRSAKey(t).N
	for _, pair := range [][2]*big.Int{
		{big.NewInt(17), big.NewInt(65537)},
		{validN, big.NewInt(0)}, {validN, big.NewInt(1)}, {validN, big.NewInt(4)},
		{validN, new(big.Int).Lsh(big.NewInt(1), 32)},
	} {
		if _, err := parseRSAPublicKey(encode(pair[0]), encode(pair[1])); err == nil {
			t.Fatal("weak RSA parameters accepted")
		}
	}
}

func TestScopePublicationRejectsAncestorSymlink(t *testing.T) {
	root := t.TempDir()
	other := filepath.Join(root, "other")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(root, "programs")); err != nil {
		t.Skip(err)
	}
	if err := publishRemoteScopeManifest(root, filepath.Join("programs", "demo", "scope.json"), []byte("{}")); err == nil {
		t.Fatal("ancestor symlink accepted")
	}
	entries, err := os.ReadDir(other)
	if err != nil || len(entries) != 0 {
		t.Fatal("publication modified the symlink destination")
	}
}

func TestRemoteIngestionChecksProgramGrantBeforeWriting(t *testing.T) {
	worker, root := specWorker(t, newMemoryQdrant())
	claims := &HiveClaims{Sub: "member-1", Permissions: []string{permissionMindRead, permissionMindIngest, permissionScopeAdmin}}
	report := worker.IngestRemoteDocument(context.Background(), claims, remoteNote())
	if report.OK || report.ExitCode != ExitAuthorization {
		t.Fatalf("missing program grant accepted: %+v", report)
	}
	files, _ := filepath.Glob(filepath.Join(root, "programs", "*", "imports", "remote", "*", "*.json"))
	if len(files) != 0 {
		t.Fatal("unauthorized ingestion wrote a document")
	}
}
