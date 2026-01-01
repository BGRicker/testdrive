package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcher_New(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(Options{
		Root:          tmpDir,
		DebounceDelay: 100 * time.Millisecond,
		OnChange:      func(paths []string) {},
	})

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if w == nil {
		t.Fatal("New() returned nil watcher")
	}

	defer w.Stop()

	if w.root != tmpDir {
		t.Errorf("root = %v, want %v", w.root, tmpDir)
	}

	if w.debounceDelay != 100*time.Millisecond {
		t.Errorf("debounceDelay = %v, want %v", w.debounceDelay, 100*time.Millisecond)
	}

	// Should have default ignore patterns
	if len(w.ignorePatterns) == 0 {
		t.Error("expected default ignore patterns, got none")
	}
}

func TestWatcher_DefaultDebounceDelay(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(Options{
		Root:     tmpDir,
		OnChange: func(paths []string) {},
	})

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer w.Stop()

	if w.debounceDelay != 300*time.Millisecond {
		t.Errorf("debounceDelay = %v, want default 300ms", w.debounceDelay)
	}
}

func TestWatcher_StartStop(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(Options{
		Root:     tmpDir,
		OnChange: func(paths []string) {},
	})

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := w.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestWatcher_DetectsFileChanges(t *testing.T) {
	tmpDir := t.TempDir()

	// Channel to receive onChange notifications
	changeChan := make(chan []string, 1)

	w, err := New(Options{
		Root:          tmpDir,
		DebounceDelay: 50 * time.Millisecond, // Short delay for tests
		OnChange: func(paths []string) {
			changeChan <- paths
		},
	})

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer w.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Give the watcher time to set up
	time.Sleep(100 * time.Millisecond)

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Wait for onChange callback
	select {
	case paths := <-changeChan:
		if len(paths) == 0 {
			t.Error("expected file paths, got empty slice")
		}
		found := false
		for _, p := range paths {
			if p == testFile {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected path %q in changes, got %v", testFile, paths)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timeout waiting for file change notification")
	}
}

func TestWatcher_IgnorePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create node_modules directory (should be ignored by default)
	nodeModules := filepath.Join(tmpDir, "node_modules")
	if err := os.Mkdir(nodeModules, 0755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	changeChan := make(chan []string, 1)

	w, err := New(Options{
		Root:          tmpDir,
		DebounceDelay: 50 * time.Millisecond,
		OnChange: func(paths []string) {
			changeChan <- paths
		},
	})

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer w.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Create file in node_modules (should be ignored)
	ignoredFile := filepath.Join(nodeModules, "test.js")
	if err := os.WriteFile(ignoredFile, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Should not receive any notifications
	select {
	case paths := <-changeChan:
		t.Errorf("expected no notifications for ignored path, got %v", paths)
	case <-time.After(200 * time.Millisecond):
		// Success - no notification received
	}
}

func TestWatcher_Debouncing(t *testing.T) {
	tmpDir := t.TempDir()

	changeCount := 0
	changeChan := make(chan []string, 5)

	w, err := New(Options{
		Root:          tmpDir,
		DebounceDelay: 100 * time.Millisecond,
		OnChange: func(paths []string) {
			changeCount++
			changeChan <- paths
		},
	})

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer w.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Create multiple files quickly (should be debounced into one notification)
	for i := 0; i < 5; i++ {
		testFile := filepath.Join(tmpDir, "test"+string(rune('0'+i))+".txt")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		time.Sleep(20 * time.Millisecond) // Less than debounce delay
	}

	// Wait for debounce period
	time.Sleep(200 * time.Millisecond)

	// Should receive only one notification despite multiple file changes
	if changeCount != 1 {
		t.Errorf("expected 1 notification due to debouncing, got %d", changeCount)
	}

	// Verify we got paths in the notification
	select {
	case paths := <-changeChan:
		if len(paths) == 0 {
			t.Error("expected file paths in debounced notification, got empty slice")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for debounced notification")
	}
}

func TestShouldIgnore(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(Options{
		Root:     tmpDir,
		OnChange: func(paths []string) {},
		IgnorePatterns: []string{
			"**/*.log",
			"**/tmp/**",
		},
	})

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer w.Stop()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "log file should be ignored",
			path:     filepath.Join(tmpDir, "test.log"),
			expected: true,
		},
		{
			name:     "file in tmp should be ignored",
			path:     filepath.Join(tmpDir, "tmp", "test.txt"),
			expected: true,
		},
		{
			name:     "regular file should not be ignored",
			path:     filepath.Join(tmpDir, "test.txt"),
			expected: false,
		},
		{
			name:     "js file should not be ignored",
			path:     filepath.Join(tmpDir, "test.js"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := w.shouldIgnore(tt.path)
			if result != tt.expected {
				t.Errorf("shouldIgnore(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestShouldInclude(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(Options{
		Root:     tmpDir,
		OnChange: func(paths []string) {},
		IncludePatterns: []string{
			"**/*.go",
			"**/*.yaml",
		},
	})

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer w.Stop()

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "go file should be included",
			path:     filepath.Join(tmpDir, "test.go"),
			expected: true,
		},
		{
			name:     "yaml file should be included",
			path:     filepath.Join(tmpDir, "config.yaml"),
			expected: true,
		},
		{
			name:     "js file should not be included",
			path:     filepath.Join(tmpDir, "test.js"),
			expected: false,
		},
		{
			name:     "txt file should not be included",
			path:     filepath.Join(tmpDir, "test.txt"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := w.shouldInclude(tt.path)
			if result != tt.expected {
				t.Errorf("shouldInclude(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestShouldInclude_NoPatterns(t *testing.T) {
	tmpDir := t.TempDir()

	w, err := New(Options{
		Root:     tmpDir,
		OnChange: func(paths []string) {},
		// No include patterns - should include everything
	})

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer w.Stop()

	// When no include patterns are specified, all files should be included
	testPath := filepath.Join(tmpDir, "test.anything")
	if !w.shouldInclude(testPath) {
		t.Error("expected all files to be included when no include patterns specified")
	}
}
