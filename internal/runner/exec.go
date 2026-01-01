package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/bgricker/testdrive/internal/config"
	"github.com/bgricker/testdrive/internal/output"
	"github.com/bgricker/testdrive/internal/provider"
	"github.com/bgricker/testdrive/internal/report"
	"github.com/bgricker/testdrive/internal/smartfilter"
)

// Options configure how the runner executes steps.
type Options struct {
	Root               string
	Stdout             io.Writer
	Stderr             io.Writer
	Verbose            bool
	DryRun             bool
	TailLines          int
	Env                []string
	Now                func() time.Time
	AllowPrivileged    bool
	PrivilegedPatterns []string
	Streaming          bool
	StreamingRenderer  output.StreamingRenderer
	SkipSteps          []string // Step name patterns to skip (supports wildcards)
	UseLocalEnv        bool     // If true, ignore workflow env variables and use only local environment
	AutoFix            bool     // If true, transform lint commands to auto-fix mode
	AutoFixRules       []config.AutoFixRule
	SmartFilter        bool     // If true, only run tests related to changed files
	SmartFilterFiles   []string // List of test files to run when SmartFilter is enabled
	SmartFilterChanged []string // List of actual changed files (for linters)
}

// Runner executes workflow steps sequentially.
type Runner struct {
	opts Options
}

// New creates a runner with the supplied options.
func New(opts Options) *Runner {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.TailLines <= 0 {
		opts.TailLines = 20
	}
	if opts.Env == nil {
		opts.Env = os.Environ()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.PrivilegedPatterns == nil || len(opts.PrivilegedPatterns) == 0 {
		opts.PrivilegedPatterns = DefaultPrivilegedPatterns()
	}
	opts.PrivilegedPatterns = append([]string{}, opts.PrivilegedPatterns...)
	
    // Streaming requires a renderer; callers should set both together.
    // Validation is handled by `cmd` layer; avoid duplicating checks here.
	
	return &Runner{opts: opts}
}

// Run executes the provided workflows returning step results and a summary.
func (r *Runner) Run(workflows []provider.Workflow) ([]report.StepResult, report.Summary, error) {
	if r.opts.Streaming {
		return r.runStreaming(workflows)
	}
	return r.runBatch(workflows)
}

// runStreaming executes workflows with real-time streaming updates.
func (r *Runner) runStreaming(workflows []provider.Workflow) ([]report.StepResult, report.Summary, error) {
	summary := report.Summary{TotalWorkflows: len(workflows)}
	results := make([]report.StepResult, 0)

    // Initialize all jobs upfront via the renderer interface
    if r.opts.StreamingRenderer != nil {
        _ = r.opts.StreamingRenderer.InitializeAllJobs(workflows)
        // Optionally start a live timer if supported
        if timer, ok := r.opts.StreamingRenderer.(output.TimerController); ok {
            timer.StartTimer()
            defer timer.StopTimer()
        }
    }

	for _, wf := range workflows {
		summary.TotalJobs += len(wf.Jobs)
		for _, job := range wf.Jobs {
            // Start the job via the interface
            if r.opts.StreamingRenderer != nil {
                _ = r.opts.StreamingRenderer.StartJob(job.Name)
            }
            // All jobs have already been registered with the renderer at the start; no need to register again here

			for _, step := range job.Steps {
				if step.Run == "" || step.Uses != "" {
					continue
				}
				summary.TotalSteps++

				result := report.StepResult{
					WorkflowPath: wf.Path,
					WorkflowName: wf.Name,
					JobName:      job.Name,
					StepName:     step.Name,
					StepRun:      step.Run,
					DryRun:       r.opts.DryRun,
				}

				// Start step
				if err := r.opts.StreamingRenderer.StartStep(step.Name); err != nil {
					return nil, summary, err
				}

				if msg, skip := shouldSkipStep(step.Run, r.opts); skip {
					result.Status = "skipped"
					result.Stderr = msg
					summary.Skipped++
					results = append(results, result)
					if err := r.opts.StreamingRenderer.CompleteStep(step.Name, "skipped", 0, "", msg, step.Run); err != nil {
						return nil, summary, err
					}
					continue
				}

				if r.opts.DryRun {
					result.Status = "skipped"
					summary.Skipped++
					results = append(results, result)
					if err := r.opts.StreamingRenderer.CompleteStep(step.Name, "skipped", 0, "", "", step.Run); err != nil {
						return nil, summary, err
					}
					continue
				}

				start := r.opts.Now()
				err := r.runStep(context.Background(), wf, job, step, &result)
				result.Duration = r.opts.Now().Sub(start)
				result.DurationMS = result.Duration.Milliseconds()

				if err != nil {
					result.Status = "failed"
					result.Stderr = tailLines(result.Stderr, r.opts.TailLines)
					result.Stdout = tailLines(result.Stdout, r.opts.TailLines)
					summary.Failed++
				} else {
					result.Status = "passed"
					summary.Passed++
				}

				summary.Duration += result.Duration
				if result.Status == "failed" {
					summary.ExitCode = 1
				}

				results = append(results, result)
				
				// Complete step with streaming update
				if err := r.opts.StreamingRenderer.CompleteStep(step.Name, result.Status, result.Duration, result.Stdout, result.Stderr, step.Run); err != nil {
					return nil, summary, err
				}
			}
			
			// Complete job with streaming update (after all steps in the job are done)
			if err := r.opts.StreamingRenderer.CompleteJob(); err != nil {
				return nil, summary, err
			}
		}
	}

	summary.DurationMS = summary.Duration.Milliseconds()
	
	// Render final summary
	if err := r.opts.StreamingRenderer.RenderSummary(summary); err != nil {
		return nil, summary, err
	}
	
	return results, summary, nil
}

// runBatch executes workflows in batch mode (original behavior).
func (r *Runner) runBatch(workflows []provider.Workflow) ([]report.StepResult, report.Summary, error) {
	summary := report.Summary{TotalWorkflows: len(workflows)}
	results := make([]report.StepResult, 0)

	for _, wf := range workflows {
		summary.TotalJobs += len(wf.Jobs)
		for _, job := range wf.Jobs {
			for _, step := range job.Steps {
				if step.Run == "" || step.Uses != "" {
					continue
				}
				summary.TotalSteps++

				result := report.StepResult{
					WorkflowPath: wf.Path,
					WorkflowName: wf.Name,
					JobName:      job.Name,
					StepName:     step.Name,
					StepRun:      step.Run,
					DryRun:       r.opts.DryRun,
				}

				if msg, skip := shouldSkipStep(step.Run, r.opts); skip {
					result.Status = "skipped"
					result.Stderr = msg
					summary.Skipped++
					results = append(results, result)
					continue
				}

				if r.opts.DryRun {
					result.Status = "skipped"
					summary.Skipped++
					results = append(results, result)
					continue
				}

				start := r.opts.Now()
				err := r.runStep(context.Background(), wf, job, step, &result)
				result.Duration = r.opts.Now().Sub(start)
				result.DurationMS = result.Duration.Milliseconds()

				if err != nil {
					result.Status = "failed"
					result.Stderr = tailLines(result.Stderr, r.opts.TailLines)
					result.Stdout = tailLines(result.Stdout, r.opts.TailLines)
					summary.Failed++
				} else {
					result.Status = "passed"
					summary.Passed++
				}

				summary.Duration += result.Duration
				if result.Status == "failed" {
					summary.ExitCode = 1
				}

				results = append(results, result)
			}
		}
	}

	summary.DurationMS = summary.Duration.Milliseconds()
	return results, summary, nil
}

func (r *Runner) runStep(ctx context.Context, wf provider.Workflow, job provider.Job, step provider.Step, result *report.StepResult) error {
	// Apply auto-fix transformations if enabled
	if r.opts.AutoFix && len(r.opts.AutoFixRules) > 0 {
		step.Run = applyAutoFixRules(step.Run, r.opts.AutoFixRules)
	}

	// Apply smart filtering if enabled
	if r.opts.SmartFilter {
		// Use changed files for linters, test files for tests
		filesToUse := r.opts.SmartFilterFiles
		if smartfilter.IsLinterCommand(step.Run) && len(r.opts.SmartFilterChanged) > 0 {
			filesToUse = r.opts.SmartFilterChanged
		}
		if len(filesToUse) > 0 {
			step.Run = smartfilter.FormatTestCommand(step.Run, filesToUse, r.opts.Root)
		}
	}

	// Auto-detect: if job has services, use local env (since testdrive can't run services)
	useLocalEnv := r.opts.UseLocalEnv || job.HasServices

	var env []string
	if useLocalEnv {
		// Use only local environment, ignore workflow/job/step env variables
		env = r.opts.Env
	} else {
		// Merge workflow, job, and step env with base environment
		env = mergeEnv(r.opts.Env, wf.Env, job.Env, step.Env)
	}
	cmdArgs, err := buildCommand(step, job, wf, env)
	if err != nil {
		result.Stderr = err.Error()
		result.ExitCode = 127
		return err
	}

	workingDir, err := resolveWorkingDirectory(r.opts.Root, wf, job, step)
	if err != nil {
		result.Stderr = err.Error()
		result.ExitCode = 127
		return err
	}

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = workingDir
	cmd.Env = env

	var stdoutBuf, stderrBuf strings.Builder
	if r.opts.Verbose {
		cmd.Stdout = io.MultiWriter(r.opts.Stdout, &stdoutBuf)
		cmd.Stderr = io.MultiWriter(r.opts.Stderr, &stderrBuf)
	} else {
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf
	}

	err = cmd.Run()
	result.Stdout = stdoutBuf.String()
	result.Stderr = simplifyError(stderrBuf.String())
	result.ExitCode = exitCode(err)

	if err != nil {
		// ensure stderr populated for messaging when verbose life.
		if !r.opts.Verbose {
			// nothing extra needed, tail applied by caller
		}
		return err
	}
	return nil
}

func buildCommand(step provider.Step, job provider.Job, wf provider.Workflow, env []string) ([]string, error) {
	shell := strings.TrimSpace(step.Shell)
	if shell == "" {
		shell = strings.TrimSpace(job.Defaults.RunShell)
	}
	if shell == "" {
		shell = strings.TrimSpace(wf.Defaults.RunShell)
	}

	return commandArgs(shell, step.Run, env)
}

func commandArgs(shellSpec string, script string, env []string) ([]string, error) {
	if shellSpec == "" {
		if runtime.GOOS == "windows" {
			return []string{"cmd", "/C", script}, nil
		}
		// Use bash with login shell and source asdf if available
		// This ensures tools like asdf, rbenv, etc. work properly
		asdfInit := getAsdfInit(env, "bash")
		return []string{"bash", "-l", "-c", asdfInit + " " + script}, nil
	}

	fields := strings.Fields(shellSpec)
	shell := fields[0]
	args := append([]string{}, fields[1:]...)
	base := strings.ToLower(filepath.Base(shell))

	switch base {
	case "bash", "zsh", "ksh", "fish":
		// These shells support login flag, use it for proper environment inheritance
		asdfInit := getAsdfInit(env, base)
		args = append(args, "-l", "-c", asdfInit + " " + script)
		return append([]string{shell}, args...), nil
	case "sh":
		// sh might be dash or another shell that doesn't support -l, use only -c
		// Also use POSIX-compliant asdf initialization
		asdfInit := getAsdfInit(env, "sh")
		args = append(args, "-c", asdfInit + " " + script)
		return append([]string{shell}, args...), nil
	case "cmd", "cmd.exe":
		args = append(args, "/C", script)
		return append([]string{shell}, args...), nil
	case "pwsh", "powershell", "powershell.exe":
		args = append(args, "-Command", script)
		return append([]string{shell}, args...), nil
	case "python", "python3", "python.exe":
		args = append(args, "-c", script)
		return append([]string{shell}, args...), nil
	default:
		args = append(args, script)
		return append([]string{shell}, args...), nil
	}
}

func resolveWorkingDirectory(root string, wf provider.Workflow, job provider.Job, step provider.Step) (string, error) {
	candidates := []string{step.WorkingDirectory, job.Defaults.WorkingDirectory, wf.Defaults.WorkingDirectory}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}

		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(root, candidate)
		}
		info, err := os.Stat(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("working directory %q not found", candidate)
			}
			return "", fmt.Errorf("stat working directory %q: %w", candidate, err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("working directory %q is not a directory", candidate)
		}
		return candidate, nil
	}
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("determine working directory: %w", err)
		}
	}
	return root, nil
}

func mergeEnv(base []string, overlays ...map[string]string) []string {
	envMap := make(map[string]string, len(base)+len(overlays)*4)
	for _, kv := range base {
		if idx := strings.Index(kv, "="); idx != -1 {
			key := kv[:idx]
			envMap[key] = kv[idx+1:]
		}
	}
	for _, overlay := range overlays {
		for k, v := range overlay {
			envMap[k] = v
		}
	}
	keys := make([]string, 0, len(envMap))
	for k := range envMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s=%s", k, envMap[k]))
	}
	return out
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(interface{ ExitStatus() int }); ok {
			return status.ExitStatus()
		}
		return exitErr.ExitCode()
	}
	return 1
}

func tailLines(input string, maxLines int) string {
	if input == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(input, "\n"), "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func shouldSkipStep(script string, opts Options) (string, bool) {
	if opts.AllowPrivileged {
		return "", false
	}
	for _, pattern := range opts.PrivilegedPatterns {
		if pattern == "" {
			continue
		}
		matched, err := regexp.MatchString(pattern, script)
		if err != nil {
			continue
		}
		if matched {
            return fmt.Sprintf("skipped privileged command matching pattern %q; set TESTDRIVE_ALLOW_PRIVILEGED=1 to run", pattern), true
		}
	}
	return "", false
}

var bundlerVersionRegex = regexp.MustCompile(`bundler' \((\d+\.\d+(?:\.\d+)?)\)`)

func simplifyError(stderr string) string {
	lower := strings.ToLower(stderr)
	if strings.Contains(lower, "could not find 'bundler'") {
		version := parseBundlerVersion(stderr)
		if version != "" {
			return fmt.Sprintf("missing bundler %s; run `gem install bundler:%s` or `bundle update --bundler`", version, version)
		}
		return "missing bundler; run `gem install bundler` or `bundle update --bundler`"
	}
	return stderr
}

func parseBundlerVersion(stderr string) string {
	match := bundlerVersionRegex.FindStringSubmatch(stderr)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func getEnvValue(env []string, key string) string {
	for _, kv := range env {
		if idx := strings.Index(kv, "="); idx != -1 && kv[:idx] == key {
			return kv[idx+1:]
		}
	}
	return ""
}

func getAsdfInit(env []string, shellBase string) string {
	// Determine asdf script path
	var asdfPath string
	// Check ASDF_DIR from environment first
	if asdfDir := getEnvValue(env, "ASDF_DIR"); asdfDir != "" {
		// Use filepath.Join for safe path construction and validate the path
		asdfPath = filepath.Join(asdfDir, "asdf.sh")
		if _, err := os.Stat(asdfPath); err != nil {
			asdfPath = ""
		}
	}
	// Fallback to HOME from environment, then os.UserHomeDir()
	if asdfPath == "" {
		home := getEnvValue(env, "HOME")
		if home == "" {
			if homeDir, err := os.UserHomeDir(); err == nil {
				home = homeDir
			}
		}
		if home != "" {
			asdfPath = filepath.Join(home, ".asdf", "asdf.sh")
			if _, err := os.Stat(asdfPath); err != nil {
				asdfPath = ""
			}
		}
	}
	if asdfPath == "" {
		return ""
	}
	// Return shell-specific initialization string
	switch shellBase {
	case "bash", "zsh":
		return fmt.Sprintf("source %q && ", asdfPath)
	case "ksh", "sh":
		return fmt.Sprintf(". %q && ", asdfPath)
	case "fish":
		// fish uses different syntax and file extension
		fishPath := strings.TrimSuffix(asdfPath, ".sh") + ".fish"
		if _, err := os.Stat(fishPath); err == nil {
			return fmt.Sprintf("source %q; ", fishPath)
		}
		// Fallback to bash script if fish version doesn't exist
		return fmt.Sprintf("source %q; ", asdfPath)
	default:
		// For unknown shells, skip asdf initialization to avoid errors
		return ""
	}
}

func DefaultPrivilegedPatterns() []string {
	return []string{
		`(?i)^sudo\b`,           // sudo commands
		`(?i)\bapt-get\b`,       // Debian/Ubuntu package manager
		`(?i)\bapt\b`,           // Modern apt command
		`(?i)\byum\b`,           // Red Hat package manager
		`(?i)\bdnf\b`,           // Fedora package manager
		`(?i)\bzypper\b`,        // SUSE package manager
		`(?i)\bpacman\b`,        // Arch package manager
		`(?i)\bbrew\b`,          // macOS package manager (can require sudo)
		`(?i)\bchoco\b`,         // Windows package manager
		`(?i)\bwinget\b`,        // Windows package manager
		`(?i)\bpip\s+install\s+--user`, // pip install --user (can require sudo)
		`(?i)\bnpm\s+install\s+-g`,     // npm install -g (can require sudo)
		`(?i)\byarn\s+global`,          // yarn global (can require sudo)
	}
}

// applyAutoFixRules transforms a command for auto-fixing based on configured rules.
// Only the first matching rule is applied.
func applyAutoFixRules(command string, rules []config.AutoFixRule) string {
	for _, rule := range rules {
		if rule.Pattern == "" {
			continue
		}

		// Check if command matches this rule's pattern using word boundaries
		pattern := `\b` + regexp.QuoteMeta(rule.Pattern) + `\b`
		matched, err := regexp.MatchString(pattern, command)
		if err != nil || !matched {
			continue
		}

		// If Replace is set, use complete replacement
		if rule.Replace != "" {
			return rule.Replace
		}

		// Otherwise, apply flag transformations
		result := command

		// Remove flags (just the flag itself, not arguments)
		for _, flag := range rule.RemoveFlags {
			if flag == "" {
				continue
			}
			// Remove flag at start of command (flag + any trailing whitespace)
			startPattern := regexp.MustCompile(`^` + regexp.QuoteMeta(flag) + `\s+`)
			result = startPattern.ReplaceAllString(result, "")
			// Remove flag in middle/end (preceding whitespace + flag)
			midPattern := regexp.MustCompile(`\s+` + regexp.QuoteMeta(flag) + `\b`)
			result = midPattern.ReplaceAllString(result, "")
		}

		// Add flags at the end
		for _, flag := range rule.AddFlags {
			if flag == "" {
				continue
			}
			// Only add if flag isn't already present (using word boundaries)
			flagPattern := `\b` + regexp.QuoteMeta(flag) + `\b`
			if matched, _ := regexp.MatchString(flagPattern, result); !matched {
				result = strings.TrimSpace(result) + " " + flag
			}
		}

		return strings.TrimSpace(result)
	}

	return command
}
