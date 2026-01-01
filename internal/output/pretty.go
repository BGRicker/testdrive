package output

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bgricker/testdrive/internal/provider"
	"github.com/bgricker/testdrive/internal/report"
	"golang.org/x/term"
)

// StreamingRenderer interface for real-time step updates.
type StreamingRenderer interface {
	InitializeAllJobs(workflows []provider.Workflow) error
	StartJob(jobName string) error
	InitializeWorkflow(workflowName, jobName string, stepCount int) error
	StartStep(stepName string) error
	CompleteStep(stepName string, status string, duration time.Duration, stdout, stderr, command string) error
	CompleteJob() error
	RenderSummary(summary report.Summary) error
}

// TimerController is an optional interface for renderers that support a live timer.
type TimerController interface {
	StartTimer()
	StopTimer()
}

// PrettyRenderer renders execution results in a human-friendly format.
type PrettyRenderer struct {
	out io.Writer
}

// StreamingPrettyRenderer renders execution results with real-time updates like GitHub CI.
type StreamingPrettyRenderer struct {
	out             io.Writer
	workflows       []workflowInfo
	currentWorkflow int
	currentJob      int
	jobOrder        []jobLocation
	supportsRefresh bool
	renderedLines   int
}

type workflowInfo struct {
	name string
	jobs []jobInfo
}

type jobInfo struct {
	name         string
	status       string
	startTime    time.Time
	duration     time.Duration
	steps        []stepResult
	detailsShown bool
	printed      bool
}

type jobLocation struct {
	workflow int
	job      int
}

type stepResult struct {
	name     string
	status   string
	duration time.Duration
	stderr   string
	stdout   string
	command  string
}

// NewPretty creates a PrettyRenderer writing to the provided writer.
func NewPretty(out io.Writer) *PrettyRenderer {
	return &PrettyRenderer{out: out}
}

// NewStreamingPretty creates a StreamingPrettyRenderer for real-time updates.
func NewStreamingPretty(out io.Writer) *StreamingPrettyRenderer {
	return &StreamingPrettyRenderer{out: out}
}

// RenderList renders workflows/jobs/steps in list mode.
func (p *PrettyRenderer) RenderList(workflows []provider.Workflow) error {
	for _, wf := range workflows {
		if _, err := fmt.Fprintf(p.out, "Workflow %s\n", decorateName(wf.Name, wf.Path)); err != nil {
			return err
		}
		for _, job := range wf.Jobs {
			if _, err := fmt.Fprintf(p.out, "  Job %s\n", job.Name); err != nil {
				return err
			}
			for _, step := range job.Steps {
				label := step.Name
				if label == "" {
					label = step.Run
				}
				if step.Run == "" {
					continue
				}
				if _, err := fmt.Fprintf(p.out, "    • %s\n", label); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// RenderResults shows execution outcomes for steps with a summary.
func (p *PrettyRenderer) RenderResults(results []report.StepResult, summary report.Summary) error {
	type key struct {
		workflow string
		job      string
	}

	var current key
	var buffer bytes.Buffer

	flush := func() error {
		if buffer.Len() == 0 {
			return nil
		}
		if _, err := buffer.WriteTo(p.out); err != nil {
			return err
		}
		buffer.Reset()
		return nil
	}

	for _, res := range results {
		k := key{workflow: res.WorkflowName, job: res.JobName}
		if current != k {
			if err := flush(); err != nil {
				return err
			}
			current = k
			fmt.Fprintf(&buffer, "Workflow %s\n", decorateName(res.WorkflowName, res.WorkflowPath))
			fmt.Fprintf(&buffer, "  Job %s\n", res.JobName)
		}

		statusSymbol := statusGlyph(res.Status)
		duration := formatDuration(res.Duration)
		label := res.StepName
		if label == "" {
			label = res.StepRun
		}
		fmt.Fprintf(&buffer, "    %s %s (%s)\n", statusSymbol, label, duration)
		if res.Status == "failed" && res.Stderr != "" {
			fmt.Fprintf(&buffer, "      stderr: %s\n", indent(res.Stderr, "      "))
		}
		if res.Status == "skipped" && res.Stderr != "" {
			fmt.Fprintf(&buffer, "      note: %s\n", indent(res.Stderr, "      "))
		}
		if res.DryRun {
			fmt.Fprintf(&buffer, "      command: %s\n", res.StepRun)
		}
	}

	if err := flush(); err != nil {
		return err
	}

	fmt.Fprintf(p.out, "SUMMARY: %d passed, %d failed, %d skipped (%s)\n", summary.Passed, summary.Failed, summary.Skipped, formatDuration(summary.Duration))
	return nil
}

// InitializeAllJobs prepares workflow/job metadata for streaming output.
func (s *StreamingPrettyRenderer) InitializeAllJobs(workflows []provider.Workflow) error {
	s.workflows = make([]workflowInfo, 0, len(workflows))
	s.jobOrder = make([]jobLocation, 0)
	s.currentWorkflow = -1
	s.currentJob = -1
	s.supportsRefresh = detectRefreshSupport(s.out)
	s.renderedLines = 0

	for _, wf := range workflows {
		workflowIndex := len(s.workflows)
		workflow := workflowInfo{
			name: wf.Name,
			jobs: make([]jobInfo, 0, len(wf.Jobs)),
		}

		for _, job := range wf.Jobs {
			stepCount := 0
			for _, step := range job.Steps {
				if step.Run != "" && step.Uses == "" {
					stepCount++
				}
			}

			workflow.jobs = append(workflow.jobs, jobInfo{
				name:    job.Name,
				status:  "pending",
				steps:   make([]stepResult, 0, stepCount),
				printed: false,
			})
			jobIdx := len(workflow.jobs) - 1
			s.jobOrder = append(s.jobOrder, jobLocation{workflow: workflowIndex, job: jobIdx})
		}

		s.workflows = append(s.workflows, workflow)
	}

	if s.supportsRefresh {
		s.render()
	} else {
		for _, loc := range s.jobOrder {
			job := &s.workflows[loc.workflow].jobs[loc.job]
			fmt.Fprintf(s.out, "%s\n", formatJobLine(job))
		}
	}

	return nil
}

// StartJob marks a job as running and begins rendering its live status line.
func (s *StreamingPrettyRenderer) StartJob(jobName string) error {
	wi, ji, job := s.findJob(jobName)
	if job == nil {
		return nil
	}

	if job.status != "pending" {
		// Nothing to do if job is already running or finished.
		return nil
	}

	job.status = "running"
	job.startTime = time.Now()
	s.currentWorkflow = wi
	s.currentJob = ji

	if s.supportsRefresh {
		s.render()
	}
	return nil
}

// InitializeWorkflow is kept for interface compatibility but not used in the new approach
func (s *StreamingPrettyRenderer) InitializeWorkflow(workflowName, jobName string, stepCount int) error {
	// Jobs are already initialized upfront, this method is not used
	return nil
}

// StartStep shows a step as running with a green circle.
func (s *StreamingPrettyRenderer) StartStep(stepName string) error {
	// Don't show step details during execution - wait for job completion
	return nil
}

// CompleteStep records step results so they can be reported if a job fails.
func (s *StreamingPrettyRenderer) CompleteStep(stepName string, status string, duration time.Duration, stdout, stderr, command string) error {
	if s.currentWorkflow < 0 || s.currentJob < 0 {
		return nil
	}

	job := &s.workflows[s.currentWorkflow].jobs[s.currentJob]
	if job.status != "running" {
		return nil
	}

	job.steps = append(job.steps, stepResult{
		name:     stepName,
		status:   status,
		duration: duration,
		stderr:   stderr,
		stdout:   stdout,
		command:  command,
	})

	return nil
}

// CompleteJob shows the final job status and step details if failed.
func (s *StreamingPrettyRenderer) CompleteJob() error {
	if s.currentWorkflow < 0 || s.currentJob < 0 {
		return nil
	}

	job := &s.workflows[s.currentWorkflow].jobs[s.currentJob]
	if job.status != "running" {
		return nil
	}

	job.duration = time.Since(job.startTime)
	job.status = "passed"
	jobFailed := false
	for _, step := range job.steps {
		if step.status == "failed" {
			job.status = "failed"
			jobFailed = true
			break
		}
	}

	if s.supportsRefresh {
		job.detailsShown = jobFailed
		job.printed = true
		s.render()
	} else {
		if job.printed {
			if jobFailed && !job.detailsShown {
				for _, line := range jobDetailLines(job) {
					fmt.Fprintf(s.out, "%s\n", line)
				}
				job.detailsShown = true
			}
		} else {
			fmt.Fprintf(s.out, "%s\n", formatJobLine(job))
			if jobFailed {
				for _, line := range jobDetailLines(job) {
					fmt.Fprintf(s.out, "%s\n", line)
				}
				job.detailsShown = true
			} else {
				job.detailsShown = false
			}
			job.printed = true
		}
	}

	s.currentWorkflow = -1
	s.currentJob = -1
	return nil
}

func jobDetailLines(job *jobInfo) []string {
	var lines []string
	for _, step := range job.steps {
		var stepEmoji string
		switch step.status {
		case "passed":
			stepEmoji = "✅"
		case "failed":
			stepEmoji = "❌"
		case "skipped":
			stepEmoji = "⏭️"
		default:
			stepEmoji = "❓"
		}
		lines = append(lines, fmt.Sprintf("    %s %s (%s)", stepEmoji, step.name, formatDuration(step.duration)))

		if step.status == "failed" {
			if step.command != "" {
				lines = append(lines, fmt.Sprintf("      Command: %s", step.command))
			}

			combinedOutput := strings.TrimSuffix(step.stdout, "\n")
			if combinedOutput != "" && step.stderr != "" {
				combinedOutput += "\n"
			}
			combinedOutput += step.stderr

			cleanedOutput := cleanErrorOutput(combinedOutput)
			if cleanedOutput != "" {
				for _, line := range strings.Split(cleanedOutput, "\n") {
					trimmed := strings.TrimSpace(line)
					if trimmed == "" {
						continue
					}
					lines = append(lines, fmt.Sprintf("      %s", trimmed))
				}
			}
		}
	}
	return lines
}

func formatJobLine(job *jobInfo) string {
	switch job.status {
	case "passed":
		return fmt.Sprintf("✅ %s (%s)", job.name, formatDuration(job.duration))
	case "failed":
		return fmt.Sprintf("❌ %s (%s)", job.name, formatDuration(job.duration))
	case "running":
		return fmt.Sprintf("🟢 %s", job.name)
	case "skipped":
		return fmt.Sprintf("⏭️ %s", job.name)
	default:
		return fmt.Sprintf("⏳ %s", job.name)
	}
}

func (s *StreamingPrettyRenderer) findJob(jobName string) (int, int, *jobInfo) {
	for wi := range s.workflows {
		for ji := range s.workflows[wi].jobs {
			if s.workflows[wi].jobs[ji].name == jobName {
				return wi, ji, &s.workflows[wi].jobs[ji]
			}
		}
	}
	return -1, -1, nil
}

func detectRefreshSupport(out io.Writer) bool {
	if os.Getenv("TESTDRIVE_SIMPLE_OUTPUT") != "" {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if envTerm := os.Getenv("TERM"); envTerm == "" || envTerm == "dumb" {
		return false
	}

	// Check if the writer is actually a terminal
	if f, ok := out.(*os.File); ok {
		if f != os.Stdout && f != os.Stderr {
			return false
		}
		if !term.IsTerminal(int(f.Fd())) {
			return false
		}
	}

	return true
}

func (s *StreamingPrettyRenderer) render() {
	if !s.supportsRefresh || len(s.jobOrder) == 0 {
		return
	}

	width := 0
	if f, ok := s.out.(*os.File); ok {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil {
			width = w
		}
	}

	lines := make([]string, 0, len(s.jobOrder))
	for _, loc := range s.jobOrder {
		job := &s.workflows[loc.workflow].jobs[loc.job]
		lines = append(lines, formatJobLine(job))
		if job.detailsShown {
			lines = append(lines, jobDetailLines(job)...)
		}
	}

	newRowCount := visibleRowCount(lines, width)

	// Move cursor up to the start of the previously rendered block.
	if s.renderedLines > 0 {
		fmt.Fprintf(s.out, "\r\033[%dA", s.renderedLines)
	}

	// Clear from cursor to end of screen to avoid leftover wrapped lines.
	fmt.Fprint(s.out, "\r\033[J")

	for i, line := range lines {
		fmt.Fprint(s.out, "\r")
		fmt.Fprint(s.out, line)
		if i < len(lines)-1 {
			fmt.Fprint(s.out, "\n")
		}
	}

	fmt.Fprint(s.out, "\n")
	s.renderedLines = newRowCount
}

// RenderSummary shows the final summary
func (s *StreamingPrettyRenderer) RenderSummary(summary report.Summary) error {
	if s.supportsRefresh {
		s.render()
		fmt.Fprint(s.out, "\n")
	} else {
		fmt.Fprint(s.out, "\n")
	}
	fmt.Fprintf(s.out, "SUMMARY: %d passed, %d failed, %d skipped (%s)\n", summary.Passed, summary.Failed, summary.Skipped, formatDuration(summary.Duration))
	return nil
}

// StartTimer starts a background timer that updates running jobs with live elapsed time
// Optional timer control interface
func (s *StreamingPrettyRenderer) StartTimer() {
	// Disabled for now - timer was causing duplicate lines
	// TODO: Reintroduce timer updates using ANSI-free rendering
}

func (s *StreamingPrettyRenderer) StopTimer() {
	// Timer disabled for now
}

// cleanErrorOutput removes noise and makes error output more readable
func cleanErrorOutput(stderr string) string {
	lines := strings.Split(stderr, "\n")

	// Check if this looks like RSpec output
	if isRSpecOutput(lines) {
		return formatRSpecFailures(lines)
	}

	// Check if this looks like linter output (standard, rubocop, eslint)
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "offense") ||
			strings.Contains(lower, "rubocop") ||
			strings.Contains(lower, "standard") ||
			strings.Contains(lower, "eslint") ||
			numberedViolation.MatchString(line) { // Numbered violations
			// This is linter output - keep only the most actionable lines.
			var result []string
			lastPath := ""
			for _, l := range lines {
				trimmed := strings.TrimSpace(l)
				if trimmed == "" {
					continue
				}
				if linterFileLocation.MatchString(trimmed) {
					result = append(result, trimmed)
					lastPath = ""
					continue
				}
				if looksLikePath(trimmed) {
					lastPath = trimmed
					continue
				}
				if eslintLineLocation.MatchString(trimmed) {
					if lastPath != "" {
						result = append(result, fmt.Sprintf("%s: %s", lastPath, trimmed))
						lastPath = ""
						continue
					}
					result = append(result, trimmed)
				}
			}
			if len(result) > 0 {
				return strings.Join(result, "\n")
			}
		}
	}

	// Otherwise, use the general cleaning logic
	var cleaned []string

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines
		if line == "" {
			continue
		}

		// Skip asdf migration warnings
		if strings.Contains(line, "Bash implementation") ||
			strings.Contains(line, "Migration guide") ||
			strings.Contains(line, "asdf website") ||
			strings.Contains(line, "Source code") ||
			strings.Contains(line, "migrate to the new version") {
			continue
		}

		// Skip parser warnings
		if strings.Contains(line, "parser/current is loading parser") ||
			strings.Contains(line, "Please see https://github.com/whitequark/parser") {
			continue
		}

		// Skip config file warnings
		if strings.Contains(line, "config file has been renamed") ||
			strings.Contains(line, "is deprecated") {
			continue
		}

		// Skip shoulda-matchers warnings
		if strings.Contains(line, "Warning from shoulda-matchers") ||
			strings.Contains(line, "validate_inclusion_of") ||
			strings.Contains(line, "boolean column") ||
			strings.Contains(line, "************************************************************************") {
			continue
		}

		// Keep important error lines
		lower := strings.ToLower(line)
		if strings.Contains(lower, "failure/error:") ||
			strings.Contains(lower, "expected ") ||
			strings.Contains(lower, "got ") ||
			strings.HasPrefix(line, "# ./spec/") ||
			strings.Contains(lower, "failed") ||
			strings.Contains(lower, "error") ||
			strings.Contains(line, "FAILED") ||
			strings.Contains(lower, "aborted") ||
			strings.Contains(line, "Tasks: TOP") {
			cleaned = append(cleaned, line)
		}
	}

	// If we have cleaned lines, return them; otherwise return a simple message
	if len(cleaned) > 0 {
		return strings.Join(cleaned, "\n")
	}

	return "Step failed - output suppressed; run with --verbose for full logs"
}

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
var linterFileLocation = regexp.MustCompile(`^[^\s].*:\d+:\d+:`)
var eslintLineLocation = regexp.MustCompile(`\d+:\d+\s+(error|warning)\b`)
var numberedViolation = regexp.MustCompile(`^\s*\d+\)`)

func visibleRowCount(lines []string, width int) int {
	if width <= 0 {
		return len(lines)
	}

	total := 0
	for _, line := range lines {
		total += rowCountForLine(line, width)
	}
	return total
}

func rowCountForLine(line string, width int) int {
	if width <= 0 {
		return 1
	}

	plain := ansiRegexp.ReplaceAllString(line, "")
	cols := utf8.RuneCountInString(plain)
	if cols == 0 {
		return 1
	}

	return (cols-1)/width + 1
}

func looksLikePath(line string) bool {
	if !strings.Contains(line, "/") {
		return false
	}
	switch {
	case strings.HasSuffix(line, ".js"),
		strings.HasSuffix(line, ".jsx"),
		strings.HasSuffix(line, ".ts"),
		strings.HasSuffix(line, ".tsx"),
		strings.HasSuffix(line, ".rb"),
		strings.HasSuffix(line, ".py"),
		strings.HasSuffix(line, ".go"),
		strings.HasSuffix(line, ".css"),
		strings.HasSuffix(line, ".scss"),
		strings.HasSuffix(line, ".sass"),
		strings.HasSuffix(line, ".html"),
		strings.HasSuffix(line, ".erb"):
		return true
	default:
		return false
	}
}

// isRSpecOutput checks if the output looks like RSpec test results
func isRSpecOutput(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(line, "Failures:") ||
			strings.Contains(line, "Failed examples:") ||
			strings.Contains(line, "rspec ./spec/") ||
			strings.Contains(line, "Finished in") ||
			strings.Contains(line, "examples,") ||
			strings.Contains(line, "Failure/Error:") ||
			strings.Contains(line, ") ") { // numbered failures like "1) ..."
			return true
		}
	}
	return false
}

// formatRSpecFailures formats RSpec failure output in a clean, hierarchical way
func formatRSpecFailures(lines []string) string {
	var result []string
	var currentFailure []string
	inFailedExamples := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Skip empty lines and noise
		if line == "" ||
			strings.Contains(line, "Bash implementation") ||
			strings.Contains(line, "Migration guide") ||
			strings.Contains(line, "asdf website") ||
			strings.Contains(line, "Source code") ||
			strings.Contains(line, "migrate to the new version") ||
			strings.Contains(line, "parser/current is loading parser") ||
			strings.Contains(line, "Please see https://github.com/whitequark/parser") ||
			strings.Contains(line, "config file has been renamed") ||
			strings.Contains(line, "is deprecated") ||
			strings.Contains(line, "Warning from shoulda-matchers") ||
			strings.Contains(line, "validate_inclusion_of") ||
			strings.Contains(line, "boolean column") ||
			strings.Contains(line, "************************************************************************") ||
			strings.Contains(line, "Finished in") ||
			strings.Contains(line, "examples,") ||
			strings.Contains(line, "Randomized with seed") ||
			strings.Contains(line, "Pending:") ||
			strings.Contains(line, "Not yet implemented") ||
			strings.Contains(line, "Database connection mocking") ||
			strings.Contains(line, "# ./spec/support/database_cleaner.rb") {
			continue
		}

		// Handle the concise "Failed examples:" tail section when our tail dropped the main block
		if strings.HasPrefix(line, "Failed examples:") {
			inFailedExamples = true
			continue
		}
		if inFailedExamples {
			if strings.HasPrefix(line, "rspec ./spec/") {
				// Example format: "rspec ./spec/models/foo_spec.rb:12 # description..."
				// Trim after first space following path to keep it short
				path := line
				if hash := strings.Index(line, " # "); hash != -1 {
					path = line[len("rspec "):hash]
				} else if strings.HasPrefix(line, "rspec ") {
					path = strings.TrimPrefix(line, "rspec ")
				}
				result = append(result, fmt.Sprintf("        ❌ %s", path))
			}
			// Do not process other lines in this block
			continue
		}

		// Start of a new failure (numbered like "2) DetectMovementsJob...")
		if strings.Contains(line, ") ") && !strings.Contains(line, "Failure/Error:") {
			if len(currentFailure) > 0 {
				result = append(result, formatSingleFailure(currentFailure)...)
			}
			currentFailure = []string{line}
		} else if len(currentFailure) > 0 {
			// Continue collecting details for current failure
			if strings.Contains(line, "Failure/Error:") ||
				strings.Contains(strings.ToLower(line), "expected") ||
				strings.Contains(strings.ToLower(line), "got") ||
				strings.HasPrefix(line, "# ./spec/") {
				currentFailure = append(currentFailure, line)
			}
		}
	}

	// Handle the last failure
	if len(currentFailure) > 0 {
		result = append(result, formatSingleFailure(currentFailure)...)
	}

	if len(result) > 0 {
		return strings.Join(result, "\n")
	}

	// If we couldn't parse failures, return the raw output
	// (better to show everything than hide errors)
	var rawOutput []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" &&
			!strings.Contains(line, "Bash implementation") &&
			!strings.Contains(line, "Migration guide") &&
			!strings.Contains(line, "parser/current is loading parser") {
			rawOutput = append(rawOutput, line)
		}
	}

	if len(rawOutput) > 0 {
		return strings.Join(rawOutput, "\n")
	}

	return "RSpec tests failed"
}

// formatSingleFailure formats a single RSpec failure
func formatSingleFailure(failureLines []string) []string {
	var result []string

	for i, line := range failureLines {
		if i == 0 {
			// Extract the spec file and line number from the failure line
			// Format: "1) EspnInjuryService.get_injury_summary_for_event provides injury summary for both teams"
			// We need to extract the spec file from the stack trace later
			if strings.Contains(line, "Failure/Error:") {
				// Extract the failure message
				if idx := strings.Index(line, "Failure/Error:"); idx != -1 {
					failureMsg := strings.TrimSpace(line[idx+len("Failure/Error:"):])
					result = append(result, fmt.Sprintf("        ❌ %s", failureMsg))
				}
			}
		} else if strings.Contains(line, "expected") && strings.Contains(line, "got") {
			// This is the detailed error message
			result = append(result, fmt.Sprintf("                    %s", line))
		} else if strings.HasPrefix(line, "# ./spec/") {
			// Extract the spec file path
			specPath := strings.TrimPrefix(line, "# ./")
			result = append(result, fmt.Sprintf("        ❌ %s", specPath))
		}
	}

	return result
}

func decorateName(name, path string) string {
	if name == "" || name == path {
		return path
	}
	return fmt.Sprintf("%s (%s)", name, path)
}

func statusGlyph(status string) string {
	switch status {
	case "passed":
		return "✓"
	case "failed":
		return "✗"
	case "skipped":
		return "-"
	default:
		return "?"
	}
}

func indent(s, pad string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Truncate(time.Millisecond).String()
}
