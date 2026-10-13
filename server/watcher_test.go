package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestWatchLoopQueuesRenamedFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := &fsnotify.Watcher{Events: make(chan fsnotify.Event), Errors: make(chan error)}
	events := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		(&IngestionWorker{}).WatchLoop(ctx, watcher, events)
		close(done)
	}()

	path := filepath.Join(t.TempDir(), "moved.md")
	watcher.Events <- fsnotify.Event{Name: path, Op: fsnotify.Rename}
	select {
	case got := <-events:
		if got != path {
			t.Fatalf("queued %q, want %q", got, path)
		}
	case <-time.After(time.Second):
		t.Fatal("rename did not queue the old file path")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher did not stop")
	}
}

func TestWatchLoopStopsWhenEventQueueIsFull(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watcher := &fsnotify.Watcher{Events: make(chan fsnotify.Event), Errors: make(chan error)}
	events := make(chan string)
	done := make(chan struct{})
	go func() {
		(&IngestionWorker{}).WatchLoop(ctx, watcher, events)
		close(done)
	}()

	watcher.Events <- fsnotify.Event{Name: filepath.Join(t.TempDir(), "note.md"), Op: fsnotify.Write}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher remained blocked on the event queue")
	}
}

func TestIngestionConsumerCancelsPendingDebounce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := &IngestionWorker{Cfg: Config{DebounceDuration: time.Hour}, PendingFiles: make(map[string]time.Time)}
	events := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		worker.IngestionConsumer(ctx, events)
		close(done)
	}()

	path := filepath.Join(t.TempDir(), "note.md")
	events <- path
	deadline := time.After(time.Second)
	for {
		worker.Mu.Lock()
		_, pending := worker.PendingFiles[path]
		worker.Mu.Unlock()
		if pending {
			break
		}
		select {
		case <-deadline:
			t.Fatal("consumer did not record the pending file")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop")
	}
	worker.Mu.Lock()
	defer worker.Mu.Unlock()
	if len(worker.PendingFiles) != 0 || worker.ActiveSyncs != 0 || worker.TotalSynced != 0 {
		t.Fatalf("debounce continued after cancellation: pending=%d active=%d synced=%d", len(worker.PendingFiles), worker.ActiveSyncs, worker.TotalSynced)
	}
}

func TestIngestionConsumerClearsExpiredDebounce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker := &IngestionWorker{Cfg: Config{DebounceDuration: 5 * time.Millisecond}, PendingFiles: make(map[string]time.Time)}
	events := make(chan string, 1)
	done := make(chan struct{})
	go func() {
		worker.IngestionConsumer(ctx, events)
		close(done)
	}()

	events <- filepath.Join(t.TempDir(), "note.md")
	deadline := time.After(time.Second)
	for {
		worker.Mu.Lock()
		pending, active, synced := len(worker.PendingFiles), worker.ActiveSyncs, worker.TotalSynced
		worker.Mu.Unlock()
		if synced == 1 {
			if pending != 0 || active != 0 {
				t.Fatalf("completed debounce left pending=%d active=%d", pending, active)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("debounced event was not processed")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop after debounce")
	}
}
