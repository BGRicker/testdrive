package smartfilter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bgricker/testdrive/internal/config"
)

func TestFormatTestCommandSkipsLintCommands(t *testing.T) {
	root := t.TempDir()
	testFile := filepath.Join(root, "spec", "lib", "smoke_spec.rb")

	commands := []string{
		"bin/rails standard",
		"yarn lint",
		"npm run lint",
		"bundle exec rubocop",
		"bundle exec standard",
		"eslint src/",
	}

	for _, command := range commands {
		got := FormatTestCommand(command, []string{testFile}, root)
		if got != command {
			t.Errorf("expected command unchanged for %q, got %q", command, got)
		}
	}
}

func TestFormatTestCommandFiltersRSpec(t *testing.T) {
	root := t.TempDir()
	testFile := filepath.Join(root, "spec", "lib", "smoke_spec.rb")

	relPath, err := filepath.Rel(root, testFile)
	if err != nil {
		t.Fatalf("failed to compute relative path: %v", err)
	}

	command := "bin/rails db:setup spec"
	expected := "bundle exec rspec " + relPath

	got := FormatTestCommand(command, []string{testFile}, root)
	if got != expected {
		lower := strings.ToLower(command)
		filterable := strings.Contains(lower, "spec")
		t.Errorf("expected %q, got %q (filterable=%t, files=%d, root=%q, testFile=%q)", expected, got, filterable, len([]string{testFile}), root, testFile)
	}
}

func TestFormatTestCommandGoTestUsesRelativePackage(t *testing.T) {
	root := t.TempDir()
	pkgDir := filepath.Join(root, "pkg", "example")
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	testFile := filepath.Join(pkgDir, "example_test.go")
	if err := os.WriteFile(testFile, []byte("package example\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got := FormatTestCommand("go test ./...", []string{testFile}, root)
	want := "go test ./pkg/example"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestFindRelatedTestsUsesRules(t *testing.T) {
	root := t.TempDir()

	sourceFile := filepath.Join(root, "app", "models", "user.rb")
	testFile := filepath.Join(root, "spec", "models", "user_spec.rb")
	if err := os.MkdirAll(filepath.Dir(sourceFile), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(testFile), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(sourceFile, []byte("class User; end\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(testFile, []byte("describe User do; end\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	related, err := FindRelatedTests(root, []string{sourceFile}, config.DefaultSmartFilterRules())
	if err != nil {
		t.Fatalf("FindRelatedTests() error = %v", err)
	}
	if !containsPath(related, testFile) {
		t.Fatalf("expected related tests to include %q, got %v", testFile, related)
	}
}

func TestFindRelatedTestsKeepsDirectTestFiles(t *testing.T) {
	root := t.TempDir()
	testFile := filepath.Join(root, "spec", "lib", "smoke_spec.rb")
	if err := os.MkdirAll(filepath.Dir(testFile), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(testFile, []byte("describe 'smoke' do; end\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	related, err := FindRelatedTests(root, []string{testFile}, config.DefaultSmartFilterRules())
	if err != nil {
		t.Fatalf("FindRelatedTests() error = %v", err)
	}
	if !containsPath(related, testFile) {
		t.Fatalf("expected related tests to include %q, got %v", testFile, related)
	}
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}
