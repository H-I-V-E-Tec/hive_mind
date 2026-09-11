package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

type specRegistry struct {
	SchemaVersion    int          `json:"schema_version"`
	LastExecutedSpec string       `json:"last_executed_spec"`
	LastVerification verification `json:"last_verification"`
	Specs            []specRecord `json:"specs"`
}

type verification struct {
	VerifiedAt     string   `json:"verified_at"`
	SourceRevision string   `json:"source_revision"`
	Commands       []string `json:"commands"`
	Result         string   `json:"result"`
	VerifiedSpecs  []string `json:"verified_specs"`
}

type specRecord struct {
	ID         string   `json:"id"`
	File       string   `json:"file"`
	Version    string   `json:"version"`
	SHA256     string   `json:"sha256"`
	Status     string   `json:"status"`
	ExecutedAt *string  `json:"executed_at"`
	Commit     *string  `json:"commit"`
	Evidence   []string `json:"evidence"`
}

func TestSpecExecutionRegistry(t *testing.T) {
	registryPath := filepath.Join("..", "docs", "spec", "status.json")
	contents, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	var registry specRegistry
	if err := json.Unmarshal(contents, &registry); err != nil {
		t.Fatalf("invalid spec registry: %v", err)
	}
	if registry.SchemaVersion != 1 || len(registry.Specs) == 0 {
		t.Fatal("unsupported or empty spec registry")
	}
	if _, err := time.Parse(time.RFC3339, registry.LastVerification.VerifiedAt); err != nil ||
		registry.LastVerification.SourceRevision == "" || registry.LastVerification.Result != "passed" ||
		len(registry.LastVerification.Commands) == 0 || len(registry.LastVerification.VerifiedSpecs) == 0 {
		t.Fatal("last_verification must record a successful, reproducible verification")
	}

	versionPattern := regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	commitPattern := regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	seen := make(map[string]bool)
	lastID := ""
	var latest time.Time
	latestID := ""
	for _, spec := range registry.Specs {
		if seen[spec.ID] || spec.ID <= lastID {
			t.Fatalf("spec ids must be unique and ordered: %q", spec.ID)
		}
		seen[spec.ID] = true
		lastID = spec.ID
		if !versionPattern.MatchString(spec.Version) {
			t.Errorf("spec %s has invalid semantic version %q", spec.ID, spec.Version)
		}
		body, err := os.ReadFile(filepath.Join("..", "docs", "spec", spec.File))
		if err != nil {
			t.Errorf("spec %s file cannot be read: %v", spec.ID, err)
			continue
		}
		sum := sha256.Sum256(body)
		if actual := hex.EncodeToString(sum[:]); actual != spec.SHA256 {
			t.Errorf("spec %s changed: bump its version, update sha256, and reset/re-execute its status", spec.ID)
		}

		switch spec.Status {
		case "completed":
			if spec.ExecutedAt == nil || spec.Commit == nil || len(spec.Evidence) == 0 {
				t.Errorf("completed spec %s lacks execution provenance", spec.ID)
				continue
			}
			when, err := time.Parse(time.RFC3339, *spec.ExecutedAt)
			if err != nil || !commitPattern.MatchString(*spec.Commit) {
				t.Errorf("completed spec %s has invalid timestamp or commit", spec.ID)
				continue
			}
			if when.After(latest) {
				latest, latestID = when, spec.ID
			}
		case "pending":
			if spec.ExecutedAt != nil || spec.Commit != nil || len(spec.Evidence) != 0 {
				t.Errorf("pending spec %s must not claim execution provenance", spec.ID)
			}
		default:
			t.Errorf("spec %s has invalid status %q", spec.ID, spec.Status)
		}
	}
	if registry.LastExecutedSpec != latestID {
		t.Errorf("last_executed_spec=%q, derived latest is %q", registry.LastExecutedSpec, latestID)
	}
	for _, id := range registry.LastVerification.VerifiedSpecs {
		if !seen[id] {
			t.Errorf("last_verification references unknown spec %q", id)
		}
	}
}
