//go:build unix

package server

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestInventoryRejectsFIFOReplacementWithoutBlocking(t *testing.T) {
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
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "input.txt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := readInventoryFile(context.Background(), root, "input.txt", info, 1024, io.Discard); err == nil {
		t.Fatal("FIFO replacement was accepted")
	}
	report := BuildInventory(context.Background(), InventoryOptions{Directory: dir})
	if !report.OK || report.Files != 0 || report.SkippedEntries != 1 {
		t.Fatalf("FIFO should be skipped: %+v", report)
	}
}
