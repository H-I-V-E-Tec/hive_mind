package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestInventoryLabeledBaseline(t *testing.T) {
	root := filepath.Join("testdata", "inventory", "corpus")
	data, err := os.ReadFile(filepath.Join("testdata", "inventory", "pairs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pairs []struct {
		Left, Right, Relation string
		SameProgram           bool `json:"same_program"`
	}
	if err := json.Unmarshal(data, &pairs); err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 5 {
		t.Fatal("missing labeled baseline pairs")
	}
	for _, pair := range pairs {
		left, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pair.Left)))
		if err != nil {
			t.Fatal(err)
		}
		right, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pair.Right)))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(left, right) != (pair.Relation == "exact_duplicate") {
			t.Fatalf("incorrect equality label: %+v", pair)
		}
		if (strings.Split(pair.Left, "/")[1] == strings.Split(pair.Right, "/")[1]) != pair.SameProgram {
			t.Fatalf("incorrect program label: %+v", pair)
		}
	}
	report := BuildInventory(context.Background(), InventoryOptions{Directory: root, Details: true})
	if !report.OK || report.Files != 6 || report.HashedFiles != 6 || report.DuplicateFiles != 1 || len(report.DuplicateGroups) != 1 {
		t.Fatalf("baseline changed: %+v", report)
	}
	if !reflect.DeepEqual(report.DuplicateGroups[0].Paths, []string{"programs/demo/base.txt", "programs/demo/copy.txt"}) {
		t.Fatal("different observations or programs were grouped")
	}
}

func TestInventoryCountsFormatsAndCopiesWithinPrograms(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"programs/acme/a.txt", "programs/acme/b.TXT", "programs/other/a.txt", "unassigned.txt"} {
		writeNote(t, root, name, "synthetic-private-content")
	}
	writeNote(t, root, "programs/acme/large.pdf", strings.Repeat("x", 65))
	writeNote(t, root, "programs/acme/empty.csv", "")
	writeNote(t, root, "programs/acme/.private", "hidden")
	opts := InventoryOptions{Directory: root, HashMaxBytes: 64}
	report := BuildInventory(context.Background(), opts)
	if !report.OK || !report.ScanComplete || report.ExitCode != 0 || report.Files != 6 || report.HashedFiles != 5 || report.OverLimitFiles != 1 || report.EmptyFiles != 1 || report.SkippedEntries != 1 {
		t.Fatalf("incorrect coverage: %+v", report)
	}
	if report.DuplicateFiles != 1 || len(report.DuplicateGroups) != 1 || report.RepeatedBytes != int64(len("synthetic-private-content")) {
		t.Fatalf("copies crossed program boundaries: %+v", report)
	}
	wantFormats := []InventoryFormat{{".csv", 1, 0}, {".pdf", 1, 65}, {".txt", 4, 4 * int64(len("synthetic-private-content"))}}
	if !reflect.DeepEqual(report.Formats, wantFormats) {
		t.Fatalf("unexpected formats: %+v", report.Formats)
	}
	encoded, _ := json.Marshal(report)
	for _, private := range []string{root, "a.txt", "synthetic-private-content", "paths", "sha256"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("summary exposed private fields: %s", encoded)
		}
	}
	if again := BuildInventory(context.Background(), opts); !reflect.DeepEqual(report, again) {
		t.Fatal("unchanged inventory is not deterministic")
	}
	opts.Details, opts.ProgramID = true, "acme"
	detailed := BuildInventory(context.Background(), opts)
	if !detailed.OK || detailed.Files != 4 || !reflect.DeepEqual(detailed.DuplicateGroups[0].Paths, []string{"programs/acme/a.txt", "programs/acme/b.TXT"}) {
		t.Fatalf("program filter/details incorrect: %+v", detailed)
	}
}

func TestInventoryHonorsNestedIgnorePolicies(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, ".gitignore", "*.log\nignored/\n")
	writeNote(t, root, "programs/acme/.gitignore", "!keep.log\nprivate.txt\n")
	writeNote(t, root, "programs/acme/keep.log", "visible")
	writeNote(t, root, "programs/acme/hide.log", "hidden")
	writeNote(t, root, "programs/acme/private.txt", "hidden")
	writeNote(t, root, "programs/acme/ignored/file.txt", "hidden")
	for _, program := range []string{"", "acme"} {
		report := BuildInventory(context.Background(), InventoryOptions{Directory: root, ProgramID: program})
		if !report.OK || report.Files != 1 || report.Formats[0].Extension != ".log" {
			t.Fatalf("ignore policy bypassed: %+v", report)
		}
	}
}

func TestInventoryRejectsLinksAndUnsafeIgnorePolicies(t *testing.T) {
	root := t.TempDir()
	inside := writeNote(t, root, "programs/acme/real.txt", "synthetic observation")
	external := writeNote(t, t.TempDir(), "outside.txt", "private-external-content")
	for _, target := range []string{inside, external} {
		link := filepath.Join(root, "programs/acme", filepath.Base(target)+".link")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	report := BuildInventory(context.Background(), InventoryOptions{Directory: root})
	if !report.OK || report.HashedFiles != 1 || report.SkippedEntries != 2 {
		t.Fatalf("symlinks were followed: %+v", report)
	}
	if err := os.Symlink(external, filepath.Join(root, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	report = BuildInventory(context.Background(), InventoryOptions{Directory: root})
	if report.OK || report.ScanComplete || report.Files != 0 || report.Issues[0].Reason != "ignore_policy_unavailable" {
		t.Fatalf("unsafe policy did not stop traversal: %+v", report)
	}
}

func TestInventoryProgramFilterRejectsDirectoryAlias(t *testing.T) {
	root := t.TempDir()
	writeNote(t, root, "programs/other/a.txt", "private")
	if err := os.Symlink("other", filepath.Join(root, "programs/acme")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	report := BuildInventory(context.Background(), InventoryOptions{Directory: root, ProgramID: "acme"})
	if report.OK || report.Files != 0 || report.Issues[0].Reason != "program_directory_unavailable" {
		t.Fatalf("program filter followed alias: %+v", report)
	}
}

func TestInventoryReportsCancellationAndMissingRoot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report := BuildInventory(ctx, InventoryOptions{Directory: t.TempDir()})
	if report.OK || report.ScanComplete || report.ExitCode != ExitPartialFailure || report.HashedFiles != 0 {
		t.Fatalf("cancelled inventory reported success: %+v", report)
	}
	report = BuildInventory(context.Background(), InventoryOptions{Directory: filepath.Join(t.TempDir(), "absent"), Details: true})
	if report.OK || report.ExitCode != ExitConfiguration || report.Issues[0].Path != "" {
		t.Fatalf("missing root report incorrect: %+v", report)
	}
}

type inventoryMutatingWriter struct{ action func() }

func (w *inventoryMutatingWriter) Write(p []byte) (int, error) {
	w.action()
	return len(p), nil
}

func TestInventoryRejectsFilesChangedDuringRead(t *testing.T) {
	dir := t.TempDir()
	file := writeNote(t, dir, "input.txt", "before")
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	info, err := root.Lstat("input.txt")
	if err != nil {
		t.Fatal(err)
	}
	w := &inventoryMutatingWriter{action: func() {
		if err := os.WriteFile(file, []byte("changed content"), 0o600); err != nil {
			t.Fatal(err)
		}
	}}
	if err := readInventoryFile(context.Background(), root, "input.txt", info, 1024, w); err == nil {
		t.Fatal("changed file was accepted for duplicate matching")
	}
	if err := readInventoryFile(context.Background(), root, "../outside.txt", info, 1024, io.Discard); err == nil {
		t.Fatal("escaped root was accepted")
	}
}

func TestInventoryCLIParsing(t *testing.T) {
	opts, err := parseInventoryArgs([]string{"corpus", "--program=acme", "--details", "--hash-max-bytes=1024"})
	if err != nil || opts.Directory != "corpus" || opts.ProgramID != "acme" || !opts.Details || opts.HashMaxBytes != 1024 {
		t.Fatalf("invalid options: %+v %v", opts, err)
	}
	for _, args := range [][]string{
		{}, {"a", "b"}, {"a", "--program=../b"}, {"a", "--hash-max-bytes=0"},
		{"a", "--hash-max-bytes=1073741825"}, {"a", "--details=yes"}, {"a", "--details", "--details"},
		{"a", "--config=secret.toml"}, {"a", "--role=writer"}, {"a", "--unknown"},
	} {
		if _, err := splitCLIArgs(append([]string{"hive", "inventory"}, args...)); err == nil {
			t.Errorf("invalid inventory arguments accepted: %v", args)
		}
	}
	if _, err := splitCLIArgs([]string{"hive", "inventory", "corpus"}); err != nil {
		t.Fatal(err)
	}
	if _, err := splitCLIArgs([]string{"hive", "--role=writer", "inventory", "corpus"}); err == nil {
		t.Fatal("service flags silently accepted for offline inventory")
	}
	if _, err := splitCLIArgs([]string{"hive", "search", "acme", "inventory"}); err != nil {
		t.Fatal("inventory keyword broke ordinary search")
	}
}
