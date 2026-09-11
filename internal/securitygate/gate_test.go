package securitygate

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReleaseRequiresCurrentCompleteEvidence(t *testing.T) {
	root := t.TempDir()
	path := "docs/operations/evidence/drill.md"
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(path)), 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte("synthetic drill evidence")
	if err := os.WriteFile(filepath.Join(root, path), body, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	now := time.Now()
	digest := strings.Repeat("a", 64)
	r := Report{Version: "v0.1.0", SourceDigest: digest, ExecutedAt: now, Owner: "operator", IncidentChannel: "on-call", Profile: map[string]float64{}, Gates: map[string]Evidence{}, ThreatControls: map[string]string{}}
	for _, k := range ProfileKeys {
		r.Profile[k] = 1
	}
	r.Profile["log_retention_days"] = 30
	for _, k := range Gates {
		r.Gates[k] = Evidence{"passed", path, hex.EncodeToString(sum[:])}
	}
	for _, k := range Threats {
		r.ThreatControls[k] = "control and test reference"
	}
	if failures := Validate(root, r, "v0.1.0", digest, now); len(failures) != 0 {
		t.Fatal(failures)
	}
	r.Gates["paired_restore"] = Evidence{Status: "pending"}
	r.Profile["rpo_hours"] = 0
	r.ExecutedAt = now.Add(-31 * 24 * time.Hour)
	if failures := Validate(root, r, "v0.1.0", strings.Repeat("b", 64), now); len(failures) < 4 {
		t.Fatalf("incomplete evidence accepted: %v", failures)
	}
}

func TestDigestChangesWithProductButNotEvidence(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := Digest(root, []string{"main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("after"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := Digest(root, []string{"main.go", "docs/operations/release-evidence.json"})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("source change did not invalidate evidence")
	}
	if _, err := Read(strings.NewReader(`{"unknown":true}`)); err == nil {
		t.Fatal("unknown evidence field accepted")
	}
}
