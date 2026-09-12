package server

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"
)

// AuditUsageRow aggregates retrieval events for one day, device, program and
// tool. It only carries counters copied from audit events, never content.
type AuditUsageRow struct {
	Day           string  `json:"day"`
	DeviceID      string  `json:"device_id"`
	ProgramID     string  `json:"program_id"`
	Tool          string  `json:"tool"`
	Queries       int     `json:"queries"`
	Failed        int     `json:"failed"`
	Truncated     int     `json:"truncated"`
	Results       int     `json:"results"`
	ResponseChars int64   `json:"response_chars"`
	SourceBytes   int64   `json:"source_bytes"`
	DurationMS    int64   `json:"duration_ms"`
	SavingsRatio  float64 `json:"source_to_response_ratio"`
}

// AuditUsageReport is the output of `audit report`.
type AuditUsageReport struct {
	Since   string          `json:"since,omitempty"`
	Program string          `json:"program_id,omitempty"`
	Files   int             `json:"files"`
	Events  int             `json:"events"`
	Rows    []AuditUsageRow `json:"rows"`
	Totals  AuditUsageRow   `json:"totals"`
	Notes   []string        `json:"notes"`
}

var usageTools = map[string]bool{"hive_search": true, "hive_get_context": true}

// BuildAuditUsageReport reads every audit-*.jsonl file in the audit directory
// and aggregates retrieval usage. Lines that are not valid events are counted
// as skipped and never echoed back.
func BuildAuditUsageReport(auditDir string, since time.Time, program string) (AuditUsageReport, error) {
	if auditDir == "" {
		return AuditUsageReport{}, errors.New("HIVE_AUDIT_DIR is required for audit report")
	}
	if program != "" && !validIdentifier(program, 64) {
		return AuditUsageReport{}, errors.New("invalid program identifier")
	}
	root, err := os.OpenRoot(auditDir)
	if err != nil {
		return AuditUsageReport{}, errors.New("audit directory is unavailable")
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return AuditUsageReport{}, errors.New("audit directory is unavailable")
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return AuditUsageReport{}, errors.New("audit directory is unavailable")
	}
	report := AuditUsageReport{Program: program, Rows: []AuditUsageRow{}, Notes: []string{
		"response_chars counts characters of the serialized tool response; tokens are roughly response_chars/4.",
		"source_bytes sums the size of distinct source documents behind the returned items; 0 for documents indexed before size tracking.",
		"hive_search issued internally by hive_get_context is not counted separately.",
	}}
	if !since.IsZero() {
		report.Since = since.UTC().Format(time.RFC3339)
	}
	groups := make(map[string]*AuditUsageRow)
	skipped := 0
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, "audit-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		f, err := root.Open(name)
		if err != nil {
			return AuditUsageReport{}, errors.New("audit file is unreadable")
		}
		report.Files++
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for scanner.Scan() {
			var event AuditEvent
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				skipped++
				continue
			}
			if !usageTools[event.Action] || (program != "" && event.Program != program) {
				continue
			}
			at, err := time.Parse(time.RFC3339Nano, event.Time)
			if err != nil || (!since.IsZero() && at.Before(since)) {
				if err != nil {
					skipped++
				}
				continue
			}
			report.Events++
			day := at.UTC().Format("2006-01-02")
			key := strings.Join([]string{day, event.Device, event.Program, event.Action}, "\x00")
			row, ok := groups[key]
			if !ok {
				row = &AuditUsageRow{Day: day, DeviceID: event.Device, ProgramID: event.Program, Tool: event.Action}
				groups[key] = row
			}
			accumulate(row, event)
			accumulate(&report.Totals, event)
		}
		f.Close()
		if err := scanner.Err(); err != nil {
			return AuditUsageReport{}, errors.New("audit file is unreadable")
		}
	}
	for _, row := range groups {
		row.SavingsRatio = ratio(row.SourceBytes, row.ResponseChars)
		report.Rows = append(report.Rows, *row)
	}
	sort.Slice(report.Rows, func(i, j int) bool {
		a, b := report.Rows[i], report.Rows[j]
		if a.Day != b.Day {
			return a.Day < b.Day
		}
		if a.DeviceID != b.DeviceID {
			return a.DeviceID < b.DeviceID
		}
		if a.ProgramID != b.ProgramID {
			return a.ProgramID < b.ProgramID
		}
		return a.Tool < b.Tool
	})
	report.Totals.Day, report.Totals.DeviceID, report.Totals.ProgramID, report.Totals.Tool = "all", "all", "all", "all"
	report.Totals.SavingsRatio = ratio(report.Totals.SourceBytes, report.Totals.ResponseChars)
	if skipped > 0 {
		report.Notes = append(report.Notes, "some audit lines were unreadable and ignored")
	}
	return report, nil
}

func accumulate(row *AuditUsageRow, event AuditEvent) {
	row.Queries++
	if event.Outcome != "completed" {
		row.Failed++
	}
	if event.Truncated {
		row.Truncated++
	}
	row.Results += event.Count
	row.ResponseChars += event.ResponseChars
	row.SourceBytes += event.SourceBytes
	row.DurationMS += event.DurationMS
}

func ratio(source, response int64) float64 {
	if response <= 0 {
		return 0
	}
	return float64(source) / float64(response)
}

// parseAuditReportArgs accepts --since=YYYY-MM-DD and --program=<id>.
func parseAuditReportArgs(args []string) (time.Time, string, error) {
	var since time.Time
	program := ""
	for _, arg := range args {
		key, value, ok := strings.Cut(arg, "=")
		if !ok {
			return since, "", errors.New("flags use --key=value")
		}
		switch key {
		case "--since":
			parsed, err := time.Parse("2006-01-02", value)
			if err != nil {
				return since, "", errors.New("--since must be YYYY-MM-DD")
			}
			since = parsed.UTC()
		case "--program":
			if !validIdentifier(value, 64) {
				return since, "", errors.New("invalid program identifier")
			}
			program = value
		default:
			return since, "", errors.New("unknown flag")
		}
	}
	return since, program, nil
}
