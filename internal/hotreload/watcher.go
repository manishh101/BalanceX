package hotreload

import (
	"log"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ReloadFunc is called when the config file changes. It receives the path
// of the changed file and should return an error if the reload fails.
type ReloadFunc func(path string) error

// Watcher monitors a config file for changes and triggers a reload callback.
// It debounces rapid writes (e.g., editors that do write-rename-delete)
// to avoid triggering multiple reloads for a single save operation.
// Inspired by Traefik's file provider which watches for config changes.
type Watcher struct {
	watcher  *fsnotify.Watcher
	path     string
	onReload ReloadFunc
	done     chan struct{}
	mu       sync.Mutex
}

// NewWatcher creates a file watcher for the given config path.
// The onReload callback is called (debounced) when the file changes.
func NewWatcher(path string, onReload ReloadFunc) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		watcher:  fw,
		path:     path,
		onReload: onReload,
		done:     make(chan struct{}),
	}

	if err := fw.Add(path); err != nil {
		fw.Close()
		return nil, err
	}

	go w.run()

	log.Printf("[HOTRELOAD] Watching config file: %s", path)
	return w, nil
}

// run is the main event loop that listens for file system events.
func (w *Watcher) run() {
	// Debounce timer — wait 300ms after last event before reloading
	var timer *time.Timer
	var timerMu sync.Mutex

	resetTimer := func() {
		timerMu.Lock()
		defer timerMu.Unlock()
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(300*time.Millisecond, func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			log.Printf("[HOTRELOAD] Config file changed, reloading: %s", w.path)
			if err := w.onReload(w.path); err != nil {
				log.Printf("[HOTRELOAD] Reload failed: %v", err)
			} else {
				log.Printf("[HOTRELOAD] Config reloaded successfully")
			}
		})
	}

	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			// React to write and create events (some editors create a new file)
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				resetTimer()
			}
			// If the file is removed (some editors do remove+create), re-add the watch
			if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
				// Try to re-add the watch in case the file was recreated
				time.AfterFunc(100*time.Millisecond, func() {
					_ = w.watcher.Add(w.path)
				})
				resetTimer()
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[HOTRELOAD] Watcher error: %v", err)

		case <-w.done:
			return
		}
	}
}

// Stop stops the file watcher.
func (w *Watcher) Stop() {
	close(w.done)
	w.watcher.Close()
}
