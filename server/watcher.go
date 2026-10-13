package server

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// --- File Watcher & Ingestion Subsystems ---
func (iw *IngestionWorker) WatchLoop(ctx context.Context, watcher *fsnotify.Watcher, eventChan chan<- string) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if event.Has(fsnotify.Create) {
				info, err := os.Stat(event.Name)
				if err == nil && info.IsDir() {
					if err := iw.addWatchRecursive(watcher, event.Name); err != nil {
						log.Printf("Failed to attach watcher to new directory %s: %v", event.Name, err)
					}
					continue
				}
			}

			if iw.ShouldIgnoreFile(event.Name, false) {
				continue
			}

			// A rename reports the old path, which must be marked missing just like a remove.
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				select {
				case eventChan <- event.Name:
				case <-ctx.Done():
					return
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Fsnotify interface raised exception: %v", err)
		}
	}
}

func (iw *IngestionWorker) addWatchRecursive(watcher *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			return nil
		}

		if iw.ShouldIgnoreFile(path, true) {
			if path == root {
				return nil
			}
			return filepath.SkipDir
		}

		return watcher.Add(path)
	})
}

func (iw *IngestionWorker) IngestionConsumer(ctx context.Context, eventChan <-chan string) {
	type scheduledSync struct {
		path     string
		sequence uint64
	}
	type pendingTimer struct {
		timer    *time.Timer
		sequence uint64
	}
	timers := make(map[string]pendingTimer)
	fired := make(chan scheduledSync, 100)
	stopped := make(chan struct{})
	defer func() {
		close(stopped)
		for _, pending := range timers {
			pending.timer.Stop()
		}
		iw.Mu.Lock()
		for path := range timers {
			delete(iw.PendingFiles, path)
		}
		iw.Mu.Unlock()
	}()
	var sequence uint64

	for {
		select {
		case <-ctx.Done():
			return
		case path, ok := <-eventChan:
			if !ok {
				return
			}
			if iw.ShouldIgnoreFile(path, false) {
				continue
			}

			// Debounce frequent chunks to avoid thrashing remote CPU/Network resources
			if pending, exists := timers[path]; exists {
				pending.timer.Stop()
			}

			iw.Mu.Lock()
			iw.PendingFiles[path] = time.Now()
			iw.Mu.Unlock()

			sequence++
			scheduled := scheduledSync{path: path, sequence: sequence}
			timer := time.AfterFunc(iw.Cfg.DebounceDuration, func() {
				select {
				case fired <- scheduled:
				case <-stopped:
				}
			})
			timers[path] = pendingTimer{timer: timer, sequence: sequence}
		case scheduled := <-fired:
			pending, exists := timers[scheduled.path]
			if !exists || pending.sequence != scheduled.sequence {
				continue
			}
			delete(timers, scheduled.path)
			if ctx.Err() != nil {
				return
			}
			iw.Mu.Lock()
			delete(iw.PendingFiles, scheduled.path)
			iw.ActiveSyncs++
			iw.Mu.Unlock()
			go func(path string) {
				defer func() {
					iw.Mu.Lock()
					iw.ActiveSyncs--
					iw.TotalSynced++
					iw.Mu.Unlock()
				}()
				iw.SyncFileState(ctx, path)
			}(scheduled.path)
		}
	}
}
