package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const inventoryDefaultHashLimit int64 = 50 << 20

type InventoryOptions struct {
	Directory    string
	ProgramID    string
	HashMaxBytes int64
	Details      bool
}

type InventoryFormat struct {
	Extension string `json:"extension"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
}

type InventoryDuplicateGroup struct {
	ProgramID     string   `json:"program_id"`
	Files         int      `json:"files"`
	BytesPerFile  int64    `json:"bytes_per_file"`
	RepeatedBytes int64    `json:"repeated_bytes"`
	Paths         []string `json:"paths,omitempty"`
}

type InventoryIssue struct {
	Reason string `json:"reason"`
	Path   string `json:"path,omitempty"`
}

// InventoryReport describes local bytes, not admission, authorization, or
// unique knowledge. Hashes and document contents never leave the scanner.
type InventoryReport struct {
	SchemaVersion   int                       `json:"schema_version"`
	OK              bool                      `json:"ok"`
	ExitCode        int                       `json:"exit_code"`
	ScanComplete    bool                      `json:"scan_complete"`
	ProgramID       string                    `json:"program_id,omitempty"`
	HashMaxBytes    int64                     `json:"hash_max_bytes"`
	Files           int                       `json:"files"`
	Bytes           int64                     `json:"bytes"`
	EmptyFiles      int                       `json:"empty_files"`
	HashedFiles     int                       `json:"hashed_files"`
	HashedBytes     int64                     `json:"hashed_bytes"`
	OverLimitFiles  int                       `json:"over_limit_files"`
	SkippedEntries  int                       `json:"skipped_entries"`
	FailedEntries   int                       `json:"failed_entries"`
	DuplicateFiles  int                       `json:"duplicate_files"`
	RepeatedBytes   int64                     `json:"repeated_bytes"`
	Formats         []InventoryFormat         `json:"formats"`
	DuplicateGroups []InventoryDuplicateGroup `json:"duplicate_groups"`
	Issues          []InventoryIssue          `json:"issues"`
}

func parseInventoryArgs(args []string) (InventoryOptions, error) {
	opts := InventoryOptions{HashMaxBytes: inventoryDefaultHashLimit}
	seen := map[string]bool{}
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			if opts.Directory != "" || strings.HasPrefix(arg, "-") {
				return opts, errors.New("inventory requires one directory")
			}
			opts.Directory = arg
			continue
		}
		key, value, hasValue := strings.Cut(arg, "=")
		if seen[key] {
			return opts, errors.New("duplicate inventory option")
		}
		seen[key] = true
		switch {
		case key == "--details" && !hasValue:
			opts.Details = true
		case key == "--program" && hasValue && validIdentifier(value, 64):
			opts.ProgramID = value
		case key == "--hash-max-bytes" && hasValue:
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 1 || n > 1<<30 {
				return opts, errors.New("inventory hash limit must be between 1 byte and 1 GiB")
			}
			opts.HashMaxBytes = n
		default:
			return opts, errors.New("invalid inventory option")
		}
	}
	if opts.Directory == "" {
		return opts, errors.New("inventory requires one directory")
	}
	return opts, nil
}

// BuildInventory only reads the explicitly selected directory. It does not load
// Hive credentials, open audit storage, connect to services, or mutate sources.
func BuildInventory(ctx context.Context, opts InventoryOptions) InventoryReport {
	if opts.HashMaxBytes == 0 {
		opts.HashMaxBytes = inventoryDefaultHashLimit
	}
	report := InventoryReport{SchemaVersion: 1, ProgramID: opts.ProgramID, HashMaxBytes: opts.HashMaxBytes,
		Formats: []InventoryFormat{}, DuplicateGroups: []InventoryDuplicateGroup{}, Issues: []InventoryIssue{}}
	issue := func(name, reason string) {
		item := InventoryIssue{Reason: reason}
		if opts.Details && name != "." {
			item.Path = name
		}
		report.Issues = append(report.Issues, item)
		report.FailedEntries++
	}
	if opts.Directory == "" || opts.HashMaxBytes < 1 || opts.HashMaxBytes > 1<<30 || (opts.ProgramID != "" && !validIdentifier(opts.ProgramID, 64)) {
		issue(".", "invalid_options")
		report.ExitCode = ExitUsage
		return report
	}
	root, err := os.OpenRoot(opts.Directory)
	if err != nil {
		issue(".", "directory_unavailable")
		report.ExitCode = ExitConfiguration
		return report
	}
	defer root.Close()
	matcher := &GitIgnoreMatcher{baseDir: "."}
	formats := map[string]*InventoryFormat{}
	type groupKey struct{ program, hash string }
	groups := map[groupKey]*InventoryDuplicateGroup{}
	start := "."
	if opts.ProgramID != "" {
		start = "programs/" + opts.ProgramID
		for _, dir := range []string{"programs", start} {
			info, err := root.Lstat(filepath.FromSlash(dir))
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				issue(dir, "program_directory_unavailable")
				report.ExitCode = ExitPartialFailure
				return report
			}
		}
		// Load only ancestor policies; never open files in other programs.
		for _, dir := range []string{".", "programs"} {
			if err := loadInventoryIgnore(ctx, root, matcher, dir); err != nil {
				issue(dir, "ignore_policy_unavailable")
				report.ExitCode = ExitPartialFailure
				return report
			}
		}
	}
	err = fs.WalkDir(root.FS(), start, func(name string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			issue(name, "entry_unavailable")
			return nil
		}
		if name != "." && (strings.HasPrefix(entry.Name(), ".") || matcher.IsIgnored(filepath.FromSlash(name), entry.IsDir())) {
			report.SkippedEntries++
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || (!entry.IsDir() && !entry.Type().IsRegular()) {
			report.SkippedEntries++
			return nil
		}
		if entry.IsDir() {
			if err := loadInventoryIgnore(ctx, root, matcher, name); err != nil {
				issue(name, "ignore_policy_unavailable")
				return fs.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			issue(name, "entry_unavailable")
			return nil
		}
		report.Files++
		report.Bytes += info.Size()
		if info.Size() == 0 {
			report.EmptyFiles++
		}
		ext := strings.ToLower(path.Ext(name))
		if formats[ext] == nil {
			formats[ext] = &InventoryFormat{Extension: ext}
		}
		formats[ext].Files++
		formats[ext].Bytes += info.Size()
		if info.Size() > opts.HashMaxBytes {
			report.OverLimitFiles++
			return nil
		}
		h := sha256.New()
		if err := readInventoryFile(ctx, root, name, info, opts.HashMaxBytes, h); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			issue(name, "file_unreadable_or_changed")
			return nil
		}
		report.HashedFiles++
		report.HashedBytes += info.Size()
		parts := strings.Split(name, "/")
		// Unassigned files are counted but never matched across unknown domains.
		if len(parts) < 3 || parts[0] != "programs" || !validIdentifier(parts[1], 64) {
			return nil
		}
		key := groupKey{parts[1], hex.EncodeToString(h.Sum(nil))}
		g := groups[key]
		if g == nil {
			g = &InventoryDuplicateGroup{ProgramID: parts[1], BytesPerFile: info.Size()}
			groups[key] = g
		}
		g.Files++
		if opts.Details {
			g.Paths = append(g.Paths, name)
		}
		return nil
	})
	if err != nil {
		issue(".", "scan_cancelled_or_failed")
	}
	report.ScanComplete = err == nil && report.FailedEntries == 0
	for _, f := range formats {
		report.Formats = append(report.Formats, *f)
	}
	sort.Slice(report.Formats, func(i, j int) bool { return report.Formats[i].Extension < report.Formats[j].Extension })
	keys := make([]groupKey, 0, len(groups))
	for key, g := range groups {
		if g.Files > 1 {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].program != keys[j].program {
			return keys[i].program < keys[j].program
		}
		return keys[i].hash < keys[j].hash
	})
	for _, key := range keys {
		g := groups[key]
		g.RepeatedBytes = int64(g.Files-1) * g.BytesPerFile
		report.DuplicateFiles += g.Files - 1
		report.RepeatedBytes += g.RepeatedBytes
		report.DuplicateGroups = append(report.DuplicateGroups, *g)
	}
	report.OK = report.ScanComplete
	if !report.OK {
		report.ExitCode = ExitPartialFailure
	}
	return report
}

// Reading through os.Root prevents symlink escapes even if the directory tree
// changes during traversal. Nonblocking open prevents a replaced FIFO hanging
// the scanner. Identity, mode, size and timestamps are checked on both sides.
func readInventoryFile(ctx context.Context, root *os.Root, name string, before fs.FileInfo, maxBytes int64, dst io.Writer) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !before.Mode().IsRegular() || before.Size() > maxBytes {
		return errors.New("invalid inventory input")
	}
	f, err := root.OpenFile(filepath.FromSlash(name), inventoryReadFlags, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !sameInventoryFile(before, opened) {
		return errors.New("inventory input changed")
	}
	r := &inventoryContextReader{ctx: ctx, reader: io.LimitReader(f, maxBytes+1)}
	n, err := io.CopyBuffer(dst, r, make([]byte, 64<<10))
	if err != nil {
		return err
	}
	after, err := f.Stat()
	current, currentErr := root.Lstat(filepath.FromSlash(name))
	if err != nil || currentErr != nil || n != before.Size() || !sameInventoryFile(opened, after) || !sameInventoryFile(after, current) {
		return errors.New("inventory input changed")
	}
	return ctx.Err()
}

func sameInventoryFile(a, b fs.FileInfo) bool {
	return b.Mode().IsRegular() && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

type inventoryContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *inventoryContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func loadInventoryIgnore(ctx context.Context, root *os.Root, matcher *GitIgnoreMatcher, dir string) error {
	name := path.Join(dir, ".gitignore")
	info, err := root.Lstat(filepath.FromSlash(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var content bytes.Buffer
	if err := readInventoryFile(ctx, root, name, info, 1<<20, &content); err != nil {
		return err
	}
	base := dir
	if base == "." {
		base = ""
	}
	gi := GitIgnore{dirPath: base}
	for _, line := range strings.Split(content.String(), "\n") {
		if p, ok := compilePattern(line, base); ok {
			gi.patterns = append(gi.patterns, p)
		}
	}
	matcher.ignores = append(matcher.ignores, gi)
	return nil
}
