package smartfilter

import (
	"path/filepath"
	"strings"
	"testing"
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
