package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bgricker/testdrive/internal/config"
	"github.com/bgricker/testdrive/internal/provider"
)

func TestRunnerDryRun(t *testing.T) {
	opts := Options{DryRun: true}
	r := New(opts)
	wf := sampleWorkflow("echo hi")

	results, summary, err := r.Run([]provider.Workflow{wf})
	if err != nil {
		t.Fatalf("runner Run: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "skipped" || !results[0].DryRun {
		t.Fatalf("expected skipped dry run, got %+v", results[0])
	}
	if summary.Skipped != 1 || summary.TotalSteps != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestRunnerExecSuccess(t *testing.T) {
	root := t.TempDir()
	stdout := &bytes.Buffer{}
	r := New(Options{Root: root, Stdout: stdout})
	wf := sampleWorkflow("echo hi")

	results, summary, err := r.Run([]provider.Workflow{wf})
	if err != nil {
		t.Fatalf("runner Run: %v", err)
	}
	if summary.Passed != 1 || summary.ExitCode != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if strings.TrimSpace(results[0].Stdout) != "hi" {
		t.Fatalf("expected stdout 'hi', got %q", results[0].Stdout)
	}
}

func TestRunnerExecFailure(t *testing.T) {
	root := t.TempDir()
	r := New(Options{Root: root})
	wf := sampleWorkflow("exit 3")

	results, summary, err := r.Run([]provider.Workflow{wf})
	if err != nil {
		t.Fatalf("runner Run: %v", err)
	}
	if summary.Failed != 1 || summary.ExitCode != 1 {
		t.Fatalf("expected failure summary, got %+v", summary)
	}
	if results[0].Status != "failed" || results[0].ExitCode == 0 {
		t.Fatalf("unexpected result: %+v", results[0])
	}
}

func TestRunnerEnvMerge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("env merge test requires POSIX shell")
	}
	root := t.TempDir()
	r := New(Options{Root: root})
	wf := provider.Workflow{
		Path: "wf.yml",
		Name: "wf",
		Env:  map[string]string{"WF_VAR": "wf"},
		Jobs: []provider.Job{
			{
				Name:  "job",
				RawID: "job",
				Env:   map[string]string{"JOB_VAR": "job"},
				Steps: []provider.Step{
					{
						Name: "step",
						Run:  echoCommand(`$WF_VAR-$JOB_VAR-$STEP_VAR`),
						Env:  map[string]string{"STEP_VAR": "step"},
					},
				},
			},
		},
	}

	results, _, err := r.Run([]provider.Workflow{wf})
	if err != nil {
		t.Fatalf("runner Run: %v", err)
	}
	if want := "wf-job-step"; !strings.Contains(results[0].Stdout, want) {
		t.Fatalf("expected output %q, got %q", want, results[0].Stdout)
	}
}

func TestRunnerUseLocalEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("use local env test requires POSIX shell")
	}
	root := t.TempDir()

	// Set up local environment with a test variable
	localEnv := append(os.Environ(), "LOCAL_VAR=local")

	r := New(Options{
		Root:        root,
		UseLocalEnv: true,
		Env:         localEnv,
	})

	wf := provider.Workflow{
		Path: "wf.yml",
		Name: "wf",
		Env:  map[string]string{"WF_VAR": "workflow"},
		Jobs: []provider.Job{
			{
				Name:  "job",
				RawID: "job",
				Env:   map[string]string{"JOB_VAR": "job"},
				Steps: []provider.Step{
					{
						Name: "step",
						// Try to access both workflow env and local env
						Run:  `echo "LOCAL=${LOCAL_VAR:-UNSET} WF=${WF_VAR:-UNSET} JOB=${JOB_VAR:-UNSET} STEP=${STEP_VAR:-UNSET}"`,
						Env:  map[string]string{"STEP_VAR": "step"},
					},
				},
			},
		},
	}

	results, _, err := r.Run([]provider.Workflow{wf})
	if err != nil {
		t.Fatalf("runner Run: %v", err)
	}

	output := results[0].Stdout

	// Local env should be present
	if !strings.Contains(output, "LOCAL=local") {
		t.Errorf("expected LOCAL_VAR to be 'local', got output: %q", output)
	}

	// Workflow/job/step env should be ignored (UNSET)
	if !strings.Contains(output, "WF=UNSET") {
		t.Errorf("expected WF_VAR to be UNSET when UseLocalEnv=true, got output: %q", output)
	}
	if !strings.Contains(output, "JOB=UNSET") {
		t.Errorf("expected JOB_VAR to be UNSET when UseLocalEnv=true, got output: %q", output)
	}
	if !strings.Contains(output, "STEP=UNSET") {
		t.Errorf("expected STEP_VAR to be UNSET when UseLocalEnv=true, got output: %q", output)
	}
}

func TestRunnerUseLocalEnvFalse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("env test requires POSIX shell")
	}
	root := t.TempDir()

	// Set up local environment with a test variable
	localEnv := append(os.Environ(), "LOCAL_VAR=local")

	r := New(Options{
		Root:        root,
		UseLocalEnv: false, // Default behavior - merge environments
		Env:         localEnv,
	})

	wf := provider.Workflow{
		Path: "wf.yml",
		Name: "wf",
		Env:  map[string]string{"WF_VAR": "workflow"},
		Jobs: []provider.Job{
			{
				Name:  "job",
				RawID: "job",
				Steps: []provider.Step{
					{
						Name: "step",
						Run:  `echo "LOCAL=${LOCAL_VAR:-UNSET} WF=${WF_VAR:-UNSET}"`,
					},
				},
			},
		},
	}

	results, _, err := r.Run([]provider.Workflow{wf})
	if err != nil {
		t.Fatalf("runner Run: %v", err)
	}

	output := results[0].Stdout

	// Both local and workflow env should be present
	if !strings.Contains(output, "LOCAL=local") {
		t.Errorf("expected LOCAL_VAR to be 'local', got output: %q", output)
	}
	if !strings.Contains(output, "WF=workflow") {
		t.Errorf("expected WF_VAR to be 'workflow' when UseLocalEnv=false, got output: %q", output)
	}
}

func TestRunnerWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("working directory test uses POSIX commands")
	}
	root := t.TempDir()
	sub := filepath.Join(root, "subdir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	r := New(Options{Root: root})
	wf := provider.Workflow{
		Path: "wf.yml",
		Name: "wf",
		Jobs: []provider.Job{
			{
				Name: "job",
				Defaults: provider.Defaults{
					WorkingDirectory: "subdir",
				},
				Steps: []provider.Step{{
					Name: "pwd",
					Run:  pwdCommand(),
				}},
			},
		},
	}

	results, _, err := r.Run([]provider.Workflow{wf})
	if err != nil {
		t.Fatalf("runner Run: %v", err)
	}
	if !strings.Contains(results[0].Stdout, "subdir") {
		t.Fatalf("expected working dir output to include subdir, got %q", results[0].Stdout)
	}
}

func TestRunnerTailCapture(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tail capture test requires POSIX tools")
	}
	root := t.TempDir()
	r := New(Options{Root: root, TailLines: 2})
	wf := sampleWorkflow("printf '1\n2\n3\n'; exit 1")

	results, _, err := r.Run([]provider.Workflow{wf})
	if err != nil {
		t.Fatalf("runner Run: %v", err)
	}
	if got := strings.TrimSpace(results[0].Stdout); got != "2\n3" {
		t.Fatalf("expected tail '2\\n3', got %q", got)
	}
}

func TestRunnerSkipsPrivilegedCommands(t *testing.T) {
	root := t.TempDir()
	r := New(Options{Root: root})
	wf := sampleWorkflow("sudo apt-get update")

	results, summary, err := r.Run([]provider.Workflow{wf})
	if err != nil {
		t.Fatalf("runner Run: %v", err)
	}
	if summary.Skipped != 1 {
		t.Fatalf("expected skipped count 1, got %+v", summary)
	}
	if results[0].Status != "skipped" {
		t.Fatalf("expected step skipped, got %+v", results[0])
	}
	if !strings.Contains(results[0].Stderr, "pattern") {
		t.Fatalf("expected skip message referencing pattern, got %q", results[0].Stderr)
	}
}

func TestRunnerAllowsPrivilegedCommands(t *testing.T) {
	root := t.TempDir()
	r := New(Options{Root: root, AllowPrivileged: true})
	wf := sampleWorkflow("sudo apt-get update")

	results, summary, err := r.Run([]provider.Workflow{wf})
	if err != nil {
		t.Fatalf("runner Run: %v", err)
	}
	if summary.Skipped != 0 {
		t.Fatalf("expected no skipped steps when AllowPrivileged=true, got %+v", summary)
	}
	if results[0].Status == "skipped" {
		t.Fatalf("expected step not skipped when AllowPrivileged=true, got %+v", results[0])
	}
}

func TestSimplifyErrorBundler(t *testing.T) {
	msg := "Could not find 'bundler' (2.6.9) required by your Gemfile.lock"
	simplified := simplifyError(msg)
	if !strings.Contains(simplified, "gem install bundler:2.6.9") {
		t.Fatalf("expected actionable bundler message, got %q", simplified)
	}
}

func sampleWorkflow(script string) provider.Workflow {
	return provider.Workflow{
		Path: "wf.yml",
		Name: "workflow",
		Jobs: []provider.Job{
			{
				Name:  "job",
				RawID: "job",
				Steps: []provider.Step{{
					Name: "step",
					Run:  script,
				}},
			},
		},
	}
}

func echoCommand(arg string) string {
	return "echo " + arg
}

func pwdCommand() string {
	if runtime.GOOS == "windows" {
		return "cd"
	}
	return "pwd"
}

func TestApplyAutoFixRules(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		rules    []config.AutoFixRule
		expected string
	}{
		{
			name:    "rubocop with parallel flag",
			command: "bundle exec rubocop --parallel",
			rules: []config.AutoFixRule{
				{
					Pattern:     "rubocop",
					RemoveFlags: []string{"--parallel"},
					AddFlags:    []string{"-A"},
				},
			},
			expected: "bundle exec rubocop -A",
		},
		{
			name:    "rubocop without parallel flag",
			command: "bundle exec rubocop",
			rules: []config.AutoFixRule{
				{
					Pattern:     "rubocop",
					RemoveFlags: []string{"--parallel"},
					AddFlags:    []string{"-A"},
				},
			},
			expected: "bundle exec rubocop -A",
		},
		{
			name:    "standard with fix flag",
			command: "bundle exec standard",
			rules: []config.AutoFixRule{
				{
					Pattern:  "standard",
					AddFlags: []string{"--fix"},
				},
			},
			expected: "bundle exec standard --fix",
		},
		{
			name:    "prettier check to write",
			command: "prettier --check src/**/*.js",
			rules: []config.AutoFixRule{
				{
					Pattern:     "prettier",
					RemoveFlags: []string{"--check"},
					AddFlags:    []string{"--write"},
				},
			},
			expected: "prettier src/**/*.js --write",
		},
		{
			name:    "complete replacement",
			command: "yarn lint",
			rules: []config.AutoFixRule{
				{
					Pattern: "yarn lint",
					Replace: "yarn fix:prettier",
				},
			},
			expected: "yarn fix:prettier",
		},
		{
			name:    "no matching rule",
			command: "npm test",
			rules: []config.AutoFixRule{
				{
					Pattern:  "rubocop",
					AddFlags: []string{"-A"},
				},
			},
			expected: "npm test",
		},
		{
			name:    "eslint with fix",
			command: "eslint src/",
			rules: []config.AutoFixRule{
				{
					Pattern:  "eslint",
					AddFlags: []string{"--fix"},
				},
			},
			expected: "eslint src/ --fix",
		},
		{
			name:    "ruff check with fix",
			command: "ruff check .",
			rules: []config.AutoFixRule{
				{
					Pattern:  "ruff check",
					AddFlags: []string{"--fix"},
				},
			},
			expected: "ruff check . --fix",
		},
		{
			name:    "empty rules",
			command: "bundle exec rubocop",
			rules:   []config.AutoFixRule{},
			expected: "bundle exec rubocop",
		},
		{
			name:    "word boundary prevents partial match",
			command: "bundle exec standardrb",
			rules: []config.AutoFixRule{
				{
					Pattern:  "standard",
					AddFlags: []string{"--fix"},
				},
			},
			// "standard" pattern should NOT match "standardrb" due to word boundaries
			// Testing with single rule to verify boundary logic (actual config has both patterns)
			expected: "bundle exec standardrb",
		},
		{
			name:    "flag at start of command",
			command: "--parallel rubocop src/",
			rules: []config.AutoFixRule{
				{
					Pattern:     "rubocop",
					RemoveFlags: []string{"--parallel"},
					AddFlags:    []string{"-A"},
				},
			},
			expected: "rubocop src/ -A",
		},
		{
			name:    "flag in middle of command",
			command: "rubocop --parallel --format json src/",
			rules: []config.AutoFixRule{
				{
					Pattern:     "rubocop",
					RemoveFlags: []string{"--parallel"},
					AddFlags:    []string{"-A"},
				},
			},
			expected: "rubocop --format json src/ -A",
		},
		{
			name:    "black check mode to auto-format",
			command: "black --check src/",
			rules: []config.AutoFixRule{
				{
					Pattern:     "black",
					RemoveFlags: []string{"--check"},
				},
			},
			expected: "black src/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyAutoFixRules(tt.command, tt.rules)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestRunnerAutoFix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("auto-fix test requires POSIX shell")
	}
	root := t.TempDir()

	r := New(Options{
		Root:    root,
		AutoFix: true,
		AutoFixRules: []config.AutoFixRule{
			{
				Pattern:  "echo test",
				Replace:  "echo fixed",
			},
		},
	})

	wf := sampleWorkflow("echo test")

	results, _, err := r.Run([]provider.Workflow{wf})
	if err != nil {
		t.Fatalf("runner Run: %v", err)
	}

	// Should have executed "echo fixed" instead of "echo test"
	if !strings.Contains(results[0].Stdout, "fixed") {
		t.Errorf("expected 'fixed' in output, got: %q", results[0].Stdout)
	}
	if strings.Contains(results[0].Stdout, "test") {
		t.Errorf("expected command to be transformed to 'echo fixed', but output contains 'test': %q", results[0].Stdout)
	}
}
