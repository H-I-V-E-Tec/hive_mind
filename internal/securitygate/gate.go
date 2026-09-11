// Package securitygate validates operator evidence before a release is built.
package securitygate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var Gates = []string{"writer_reader_validation", "private_tls_network", "encryption_at_rest", "individual_revocation", "writer_promotion", "credential_rotation", "paired_restore", "audit_privacy", "two_machine_acceptance", "container_verification"}
var ProfileKeys = []string{"rpo_hours", "rto_hours", "document_retention_days", "vector_retention_days", "backup_retention_days", "log_retention_days", "prune_grace_hours", "restore_test_interval_days"}
var Threats = []string{"unauthorized_access", "traffic_interception", "data_leakage", "cross_hive_access", "prompt_injection", "unsafe_paths", "stale_revisions", "concurrent_writers", "supply_chain", "data_loss"}

type Evidence struct {
	Status string `json:"status"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}
type Report struct {
	Version         string              `json:"version"`
	SourceDigest    string              `json:"source_digest"`
	ExecutedAt      time.Time           `json:"executed_at"`
	Owner           string              `json:"owner"`
	IncidentChannel string              `json:"incident_channel"`
	Profile         map[string]float64  `json:"profile"`
	Gates           map[string]Evidence `json:"gates"`
	ThreatControls  map[string]string   `json:"threat_controls"`
}

func Read(r io.Reader) (Report, error) {
	var report Report
	d := json.NewDecoder(io.LimitReader(r, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(&report); err != nil {
		return report, errors.New("invalid security evidence JSON")
	}
	if d.Decode(new(any)) != io.EOF {
		return report, errors.New("trailing security evidence data")
	}
	return report, nil
}

func Validate(root string, r Report, version, digest string, now time.Time) []string {
	var failures []string
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[a-zA-Z0-9.-]+)?$`).MatchString(version) || r.Version != version {
		failures = append(failures, "release version mismatch")
	}
	if len(digest) != 64 || r.SourceDigest != digest {
		failures = append(failures, "source digest mismatch")
	}
	if r.ExecutedAt.IsZero() || r.ExecutedAt.After(now.Add(5*time.Minute)) || now.Sub(r.ExecutedAt) > 30*24*time.Hour {
		failures = append(failures, "operational evidence must be current (30 days)")
	}
	for _, v := range []string{r.Owner, r.IncidentChannel} {
		if strings.TrimSpace(v) == "" || strings.Contains(strings.ToUpper(v), "UNSET") {
			failures = append(failures, "operational owner and incident channel required")
		}
	}
	for _, key := range ProfileKeys {
		if r.Profile[key] <= 0 {
			failures = append(failures, "missing positive deployment parameter: "+key)
		}
	}
	if r.Profile["prune_grace_hours"] > 24 {
		failures = append(failures, "prune grace exceeds v0.1 maximum")
	}
	if r.Profile["log_retention_days"] != 30 {
		failures = append(failures, "log retention must match the implemented 30-day audit policy")
	}
	for _, name := range Gates {
		e, ok := r.Gates[name]
		if !ok || e.Status != "passed" {
			failures = append(failures, "gate not passed: "+name)
			continue
		}
		if !strings.HasPrefix(e.File, "docs/operations/evidence/") || !filepath.IsLocal(e.File) {
			failures = append(failures, "invalid evidence path: "+name)
			continue
		}
		info, err := os.Lstat(filepath.Join(root, e.File))
		if err != nil || !info.Mode().IsRegular() {
			failures = append(failures, "missing evidence file: "+name)
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, e.File))
		sum := sha256.Sum256(body)
		if err != nil || len(body) == 0 || hex.EncodeToString(sum[:]) != e.SHA256 {
			failures = append(failures, "evidence hash mismatch: "+name)
		}
	}
	for _, threat := range Threats {
		control := r.ThreatControls[threat]
		if strings.TrimSpace(control) == "" || strings.Contains(strings.ToUpper(control), "UNSET") {
			failures = append(failures, "missing threat control: "+threat)
		}
	}
	sort.Strings(failures)
	return failures
}

// Digest binds evidence to tracked product, tests, deployment and pipeline
// files, excluding only the operational report/evidence to avoid a circular hash.
func Digest(root string, paths []string) (string, error) {
	sort.Strings(paths)
	h := sha256.New()
	for _, path := range paths {
		if path == "docs/operations/release-evidence.json" || strings.HasPrefix(path, "docs/operations/evidence/") {
			continue
		}
		if !filepath.IsLocal(path) {
			return "", errors.New("invalid source path")
		}
		info, err := os.Lstat(filepath.Join(root, path))
		if err != nil {
			return "", err
		}
		var body []byte
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filepath.Join(root, path))
			if err != nil {
				return "", err
			}
			body = []byte(target)
		} else {
			body, err = os.ReadFile(filepath.Join(root, path))
			if err != nil {
				return "", err
			}
		}
		sum := sha256.Sum256(body)
		fmt.Fprintf(h, "%s\x00%o\x00%x\n", path, info.Mode().Perm(), sum)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
