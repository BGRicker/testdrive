package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/fsnotify/fsnotify"
)

// Watcher monitors files for changes and triggers callbacks.
type Watcher struct {
	watcher         *fsnotify.Watcher
	root            string
	debounceDelay   time.Duration
	ignorePatterns  []string
	includePatterns []string
	onChange        func(paths []string)

	mu            sync.Mutex
	pendingFiles  map[string]bool
	debounceTimer *time.Timer
}

// Options configure the file watcher.
type Options struct {
	Root            string
	DebounceDelay   time.Duration
	IgnorePatterns  []string
	IncludePatterns []string
	OnChange        func(paths []string)
}

// New creates a new file watcher.
func New(opts Options) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create file watcher: %w", err)
	}

	if opts.DebounceDelay == 0 {
		opts.DebounceDelay = 300 * time.Millisecond
	}

	w := &Watcher{
		watcher:         fsw,
		root:            opts.Root,
		debounceDelay:   opts.DebounceDelay,
		ignorePatterns:  opts.IgnorePatterns,
		includePatterns: opts.IncludePatterns,
		onChange:        opts.OnChange,
		pendingFiles:    make(map[string]bool),
	}

	// Add default ignore patterns if none provided
	if len(w.ignorePatterns) == 0 {
		w.ignorePatterns = defaultIgnorePatterns()
	}

	return w, nil
}

// Start begins watching for file changes.
func (w *Watcher) Start(ctx context.Context) error {
	// Walk directory tree and add watches
	if err := w.addWatches(w.root); err != nil {
		return fmt.Errorf("add watches: %w", err)
	}

	// Start event loop
	go w.eventLoop(ctx)

	return nil
}

// Stop stops the file watcher.
func (w *Watcher) Stop() error {
	w.mu.Lock()
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
	}
	w.mu.Unlock()

	return w.watcher.Close()
}

// addWatches recursively adds watches for directories.
func (w *Watcher) addWatches(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip if path matches ignore patterns
		if w.shouldIgnore(path) {
			if info != nil && info.IsDir() {
				base := filepath.Base(path)
				if base != "" && base[0] != '.' {
					// For non-hidden directories, skip the whole tree
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Only watch directories (fsnotify watches all files in a directory)
		if info != nil && info.IsDir() {
			if err := w.watcher.Add(path); err != nil {
				return fmt.Errorf("watch %q: %w", path, err)
			}
		}

		return nil
	})
}

// eventLoop processes file system events.
func (w *Watcher) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Handle new directories so we watch inside them
			if event.Op&fsnotify.Create == fsnotify.Create {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if !w.shouldIgnore(event.Name) {
						_ = w.watcher.Add(event.Name)
					}
					continue
				}
			}

			// Ignore if not a relevant operation
			if !w.isRelevantEvent(event) {
				continue
			}

			// Ignore if path doesn't match include patterns
			if !w.shouldInclude(event.Name) {
				continue
			}

			// Ignore if path matches ignore patterns
			if w.shouldIgnore(event.Name) {
				continue
			}

			// Add to pending files and reset debounce timer
			w.addPendingFile(event.Name)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			// Log error but continue watching
			fmt.Fprintf(os.Stderr, "watch error: %v\n", err)
		}
	}
}

// addPendingFile adds a file to pending changes and manages debouncing.
func (w *Watcher) addPendingFile(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pendingFiles[path] = true

	// Reset debounce timer
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
	}

	w.debounceTimer = time.AfterFunc(w.debounceDelay, func() {
		w.triggerOnChange()
	})
}

// triggerOnChange calls the onChange callback with accumulated files.
func (w *Watcher) triggerOnChange() {
	w.mu.Lock()
	files := make([]string, 0, len(w.pendingFiles))
	for path := range w.pendingFiles {
		files = append(files, path)
	}
	w.pendingFiles = make(map[string]bool)
	w.mu.Unlock()

	if len(files) > 0 && w.onChange != nil {
		w.onChange(files)
	}
}

// isRelevantEvent checks if the event should trigger a change.
func (w *Watcher) isRelevantEvent(event fsnotify.Event) bool {
	// Watch for write and create events
	return event.Op&fsnotify.Write == fsnotify.Write ||
		event.Op&fsnotify.Create == fsnotify.Create
}

// shouldIgnore checks if a path matches ignore patterns.
func (w *Watcher) shouldIgnore(path string) bool {
	relPath, err := filepath.Rel(w.root, path)
	if err != nil {
		relPath = path
	}

	for _, pattern := range w.ignorePatterns {
		matched, err := doublestar.Match(pattern, relPath)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// shouldInclude checks if a path matches include patterns.
func (w *Watcher) shouldInclude(path string) bool {
	// If no include patterns, include everything
	if len(w.includePatterns) == 0 {
		return true
	}

	relPath, err := filepath.Rel(w.root, path)
	if err != nil {
		relPath = path
	}

	for _, pattern := range w.includePatterns {
		matched, err := doublestar.Match(pattern, relPath)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// defaultIgnorePatterns returns sensible default ignore patterns.
func defaultIgnorePatterns() []string {
	return []string{
		"**/node_modules/**",
		"**/vendor/**",
		"**/tmp/**",
		"**/log/**",
		"**/.git/**",
		"**/.testdrive/**",
		"**/*.log",
		"**/.DS_Store",
		"**/coverage/**",
	}
}
