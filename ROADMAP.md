# Testdrive Roadmap - Future Features

This document outlines planned features for testdrive with implementation guidance for CLI tool development.

---

## 1. 🔄 Watch Mode - Auto-rerun on File Changes

### Feature Description
Automatically re-run tests/lints when files change, enabling fast feedback loops for TDD workflows.

### User Stories
- As a developer, I want tests to re-run automatically when I save a file, so I get instant feedback
- As a developer, I want to watch specific jobs/steps, so I only run what's relevant
- As a developer, I want smart debouncing, so the tool doesn't thrash when I'm typing

### Commands
```bash
# Watch all workflows
testdrive watch

# Watch specific job
testdrive watch --job test

# Watch specific steps
testdrive watch --only-step "Lint Ruby"

# Watch with custom patterns
testdrive watch --include "**/*.rb" --exclude "vendor/**"

# Combine with other flags
testdrive watch --auto-fix --use-local-env
```

### Configuration
```yaml
# .testdrive.yml
watch:
  debounce_ms: 300        # Wait 300ms after last change
  clear_on_run: true      # Clear terminal before each run
  ignore_patterns:
    - "tmp/**"
    - "log/**"
    - "node_modules/**"
  include_patterns:
    - "app/**/*.rb"
    - "spec/**/*.rb"
    - "lib/**/*.rb"
```

### Implementation Guidance

**Core Components:**
1. **File Watcher**: Use `fsnotify` (Go) or similar library to watch filesystem changes
2. **Debouncer**: Implement time-based debouncing to avoid excessive reruns
3. **Filter Engine**: Match changed files against include/exclude patterns
4. **Runner Integration**: Reuse existing runner with added context about what changed

**Implementation Prompts:**

```
Prompt 1: "Implement a file watcher service in Go that monitors a directory tree for changes,
with configurable debouncing (wait N milliseconds after last change before triggering).
Use the fsnotify library. Support include/exclude glob patterns."

Prompt 2: "Create a watch command for a CLI tool that:
1. Starts a file watcher on the project directory
2. When files change, determines which CI jobs/steps should run
3. Clears terminal and shows 'Running...' indicator
4. Re-runs the relevant jobs
5. Shows clear diff between runs (what passed/failed)"

Prompt 3: "Design a smart pattern matching system that maps changed files to relevant CI jobs.
For example: *.rb files should run Ruby linter, *_spec.rb should run tests. Make it configurable."
```

**Technical Considerations:**
- Handle rapid file changes (save multiple files at once)
- Gracefully handle file system events on different OS (macOS FSEvents, Linux inotify, Windows)
- Allow cancelling in-progress runs when new changes detected
- Memory-efficient watching of large directories
- Cross-platform path handling

**References:**
- `fsnotify` (Go): https://github.com/fsnotify/fsnotify
- `watchexec` (Rust): https://github.com/watchexec/watchexec (inspiration)
- `nodemon` (Node.js): https://github.com/remy/nodemon (UX reference)

---

## 2. ⚡ Parallel Job Execution

### Feature Description
Run independent jobs simultaneously to dramatically reduce workflow execution time, matching GitHub Actions' parallel behavior.

### User Stories
- As a developer, I want independent jobs to run in parallel, so my workflow completes faster
- As a developer, I want to control concurrency, so I don't overwhelm my machine
- As a developer, I want to see all jobs' status in real-time, so I know what's happening

### Commands
```bash
# Enable parallel execution (auto-detect concurrency)
testdrive run --parallel

# Limit concurrent jobs
testdrive run --parallel --max-jobs 4

# Dry-run to see execution plan
testdrive run --parallel --dry-run

# Parallel with watch mode
testdrive watch --parallel
```

### Configuration
```yaml
# .testdrive.yml
parallel:
  enabled: true
  max_jobs: 4              # Limit concurrent jobs
  respect_needs: true      # Honor job dependencies from workflow
  fail_fast: false         # Continue running other jobs if one fails
```

### Implementation Guidance

**Core Components:**
1. **Dependency Graph**: Parse `needs:` from workflows to build job dependency graph
2. **Job Scheduler**: Schedule jobs respecting dependencies and concurrency limits
3. **Worker Pool**: Manage concurrent job execution
4. **Progress Tracker**: Real-time status updates for all running jobs

**Implementation Prompts:**

```
Prompt 1: "Implement a job dependency graph builder in Go. Parse GitHub Actions workflows
and extract 'needs:' relationships between jobs. Build a directed acyclic graph (DAG) that
represents job dependencies. Detect circular dependencies and error appropriately."

Prompt 2: "Create a job scheduler that:
1. Takes a DAG of jobs with dependencies
2. Executes jobs in parallel when dependencies are met
3. Respects a max concurrency limit
4. Returns results as jobs complete
Use Go channels and goroutines for concurrent execution."

Prompt 3: "Design a real-time progress display for parallel job execution. Show:
- List of all jobs with status (queued/running/passed/failed)
- Live progress bars for running jobs
- Completion time for finished jobs
- Failed job output highlighted
Use terminal control codes for live updates."

Prompt 4: "Implement a worker pool pattern in Go with:
- Configurable number of workers
- Job queue with priorities
- Graceful shutdown when Ctrl+C is pressed
- Resource cleanup for cancelled jobs"
```

**Technical Considerations:**
- Topological sort for dependency ordering
- Deadlock detection in dependency graph
- Fair job scheduling (avoid starvation)
- Output interleaving (separate logs per job)
- Memory management (limit concurrent output buffers)
- Graceful cancellation of remaining jobs on failure

**References:**
- DAG implementation: https://github.com/yourbasic/graph
- Worker pool pattern: https://gobyexample.com/worker-pools
- GitHub Actions parallelization: https://docs.github.com/en/actions/using-jobs/using-jobs-in-a-workflow

---

## 3. 🎨 Interactive TUI (Terminal UI)

### Feature Description
Beautiful, interactive terminal interface for navigating jobs, viewing logs, and re-running specific steps.

### User Stories
- As a developer, I want a visual dashboard of all CI jobs, so I can quickly see what's failing
- As a developer, I want to navigate logs easily, so I can debug failures faster
- As a developer, I want to re-run individual steps, so I don't waste time re-running everything

### Commands
```bash
# Launch interactive mode
testdrive tui

# Launch with specific workflow
testdrive tui --workflow .github/workflows/ci.yml

# Launch in watch mode
testdrive tui --watch
```

### Interface Design
```
┌─ Testdrive ─────────────────────────────────────────────────────────┐
│ Workflow: Ruby on Rails CI                    [↑/↓: navigate] [q: quit] │
├─────────────────────────────────────────────────────────────────────┤
│ Jobs                          │ Logs                                │
│                               │                                     │
│ ✓ lint (2.4s)                │ > bin/rails standard:fix            │
│ ✗ test (34.2s)               │   Checking 243 files...             │
│   ✓ Install modules          │   app/models/user.rb:12:5: Style... │
│   ✓ Set up database          │   app/models/post.rb:45:12: Style..│
│   ✗ Run tests                │   Fixed 23 offenses                 │
│ ⊙ scan_js (running...)       │                                     │
│ ○ scan_ruby (queued)         │ [Enter: toggle] [r: rerun]         │
│                               │ [/: search] [f: filter]             │
└───────────────────────────────────────────────────────────────────────┘
Status: 1 failed, 1 running, 1 queued, 1 passed | Duration: 36.8s
```

### Key Bindings
- `↑/↓` or `j/k`: Navigate jobs/steps
- `Enter`: Expand/collapse job details
- `r`: Re-run selected job/step
- `a`: Re-run all failed jobs
- `f`: Filter by status (passed/failed/running)
- `/`: Search in logs
- `l`: View full logs in pager
- `q`: Quit

### Implementation Guidance

**Core Components:**
1. **TUI Framework**: Use `tview` or `bubbletea` for Go TUI development
2. **State Management**: Track jobs, steps, logs, and user interactions
3. **Event Loop**: Handle user input, job updates, and screen redraws
4. **Log Viewer**: Efficient scrolling and searching of large logs

**Implementation Prompts:**

```
Prompt 1: "Create a terminal UI application using the bubbletea framework in Go.
Implement a split-pane layout with:
- Left pane: Tree view of jobs and steps with status icons
- Right pane: Log output for selected job/step
- Bottom status bar: Summary and keybinding hints
Support vim-style navigation (h/j/k/l) and arrow keys."

Prompt 2: "Implement a log viewer component that:
- Streams logs in real-time as jobs run
- Supports searching with highlight
- Handles ANSI color codes correctly
- Auto-scrolls to bottom for running jobs
- Allows manual scrolling for completed jobs
- Efficiently handles large log files (>100MB)"

Prompt 3: "Design a state machine for the TUI that handles:
- User navigating between jobs/steps
- Jobs starting/completing in background
- User triggering re-runs
- Graceful shutdown on Ctrl+C
Use channels to communicate between UI and runner."

Prompt 4: "Implement keyboard shortcuts and commands:
- Vim-style navigation (j/k/h/l)
- Quick actions (r for rerun, a for rerun all failed)
- Search mode with regex support
- Filter toggles for job status
- Help modal with all keybindings"
```

**Technical Considerations:**
- Terminal resize handling
- ANSI escape code parsing for colors
- Efficient log rendering (virtual scrolling for large logs)
- Concurrent job execution while UI is responsive
- Cross-platform terminal compatibility
- Accessibility (screen readers)

**References:**
- `bubbletea` (Go): https://github.com/charmbracelet/bubbletea
- `tview` (Go): https://github.com/rivo/tview
- `k9s` (inspiration): https://k9scli.io/
- `lazygit` (inspiration): https://github.com/jesseduffield/lazygit

---

## 4. 🎯 Smart File-Based Filtering

### Feature Description
Run only the jobs/steps affected by changed files, dramatically reducing execution time for large codebases.

### User Stories
- As a developer, I want to run only tests for files I changed, so I get faster feedback
- As a developer, I want to customize which files trigger which jobs, so the mapping is accurate
- As a developer, I want to see what would run before running it, so I can verify the logic

### Commands
```bash
# Run jobs for files changed since last commit
testdrive run --changed

# Compare against specific branch
testdrive run --changed-from main

# See what would run (dry-run)
testdrive run --changed --dry-run

# Manually specify affected paths
testdrive run --affected "app/models/**" --affected "spec/models/**"

# Combine with watch mode
testdrive watch --changed
```

### Configuration
```yaml
# .testdrive.yml
changed_detection:
  default_branch: main

  # Map file patterns to jobs
  job_mappings:
    - patterns: ["app/**/*.rb", "lib/**/*.rb"]
      jobs: ["lint", "test"]

    - patterns: ["app/javascript/**/*.js"]
      jobs: ["lint", "scan_js"]

    - patterns: ["*.md", "docs/**"]
      jobs: ["lint"]
      skip_on_docs_only: true  # Skip if only docs changed

    - patterns: ["Gemfile", "Gemfile.lock"]
      jobs: ["test"]  # Dependencies changed, run all tests

  # Always run these jobs regardless of changes
  always_run:
    - "security_scan"
```

### Implementation Guidance

**Core Components:**
1. **Git Diff Parser**: Detect changed files using git diff
2. **Pattern Matcher**: Match changed files to job patterns
3. **Job Selector**: Determine which jobs should run
4. **Dry-Run Explainer**: Show why each job was selected/skipped

**Implementation Prompts:**

```
Prompt 1: "Implement a git diff analyzer in Go that:
1. Runs 'git diff' to get changed files
2. Supports comparing against different branches/commits
3. Handles renames and deletions
4. Returns list of changed file paths
5. Works with both staged and unstaged changes"

Prompt 2: "Create a pattern matching engine that:
1. Takes changed file paths and glob patterns
2. Matches paths against patterns (support ** wildcards)
3. Maps matched patterns to job names
4. Returns set of jobs that should run
5. Handles priority (more specific patterns override general ones)"

Prompt 3: "Design a configuration system for file-to-job mapping:
- Allow users to define glob patterns → jobs
- Support 'always run' jobs
- Support 'skip if only X changed' rules
- Validate configuration at startup
- Provide helpful errors for invalid patterns"

Prompt 4: "Implement a dry-run explainer that shows:
- List of changed files
- Which patterns each file matched
- Which jobs will run and why
- Which jobs are skipped and why
- Estimated time savings vs full run"
```

**Technical Considerations:**
- Handle edge cases (new files, deleted files, renamed files)
- Git submodules and worktrees
- Large diffs (thousands of changed files)
- Monorepo support (different projects in same repo)
- Cross-platform path handling
- Performance for large repositories

**References:**
- `go-git` library: https://github.com/go-git/go-git
- `doublestar` (glob matching): https://github.com/bmatcuk/doublestar
- Nx affected: https://nx.dev/concepts/affected
- Turborepo filtering: https://turbo.build/repo/docs/crafting-your-repository/running-tasks#using-filters

---

## 5. 🪝 Git Hook Integration

### Feature Description
Automatically run relevant CI checks before commits/pushes, catching issues early and reducing CI failures.

### User Stories
- As a developer, I want linters to run before committing, so I catch style issues early
- As a developer, I want quick tests before pushing, so I don't break CI
- As a developer, I want to skip hooks in emergencies, so I'm not blocked

### Commands
```bash
# Install git hooks
testdrive hooks install

# Uninstall git hooks
testdrive hooks uninstall

# Show what hooks would run
testdrive hooks info

# Test hooks without committing
testdrive hooks run pre-commit

# Skip hooks for this commit
git commit --no-verify
```

### Configuration
```yaml
# .testdrive.yml
hooks:
  pre-commit:
    # Run lints on changed files only
    - job: lint
      changed_only: true
      timeout: 2m

    # Format code automatically
    - job: format
      auto_fix: true
      changed_only: true

  pre-push:
    # Run tests before pushing
    - job: test
      changed_only: true
      timeout: 5m

    # Always run security scans
    - job: scan_ruby
      changed_only: false

  # Global settings
  allow_skip: true          # Allow --no-verify
  parallel: true            # Run hook jobs in parallel
  fail_fast: true           # Stop on first failure
```

### Implementation Guidance

**Core Components:**
1. **Hook Installer**: Write hook scripts to `.git/hooks/`
2. **Hook Runner**: Execute configured jobs/steps for each hook type
3. **Integration**: Call testdrive from git hooks with appropriate flags
4. **UI**: Show progress during git operations

**Implementation Prompts:**

```
Prompt 1: "Implement a git hook installer in Go that:
1. Detects if .git directory exists
2. Backs up existing hooks if present
3. Creates pre-commit and pre-push hook scripts
4. Makes hook scripts executable
5. Handles uninstall by restoring backups"

Prompt 2: "Create hook scripts that:
1. Call 'testdrive hooks run <hook-type>'
2. Pass changed file list to testdrive
3. Show progress with spinner/progress bar
4. Exit with appropriate code (0 = success, 1 = failure)
5. Respect --no-verify flag
6. Work on Linux/macOS/Windows"

Prompt 3: "Design a hook runner that:
1. Reads hook configuration from .testdrive.yml
2. Determines which jobs to run based on hook type
3. Filters to changed files if configured
4. Runs jobs with timeout enforcement
5. Shows clear output during git operations
6. Provides suggestions if checks fail"

Prompt 4: "Implement a pre-commit hook UI that:
- Shows 'Running pre-commit checks...' message
- Displays progress for each job (✓ passed, ✗ failed)
- Shows helpful message if checks fail
- Suggests running with --no-verify if needed
- Cleans up output (no ANSI codes in git output)"
```

**Technical Considerations:**
- Git hook script compatibility (bash/sh for Unix, batch/powershell for Windows)
- Handling failures gracefully (don't block commits indefinitely)
- Performance (pre-commit hooks should be fast)
- User can always bypass with `--no-verify`
- Multiple hook managers (Husky, pre-commit framework) compatibility
- Monorepo support (different hooks for different projects)

**References:**
- Git hooks documentation: https://git-scm.com/docs/githooks
- Husky (Node.js): https://typicode.github.io/husky/
- pre-commit (Python): https://pre-commit.com/
- lefthook (Go): https://github.com/evilmartians/lefthook

---

## Implementation Priority

### Phase 1: Quick Wins (1-2 weeks each)
1. **Git Hook Integration** - Low complexity, high value
2. **Watch Mode** - Medium complexity, immediate productivity boost

### Phase 2: Performance (2-3 weeks each)
3. **Smart File-Based Filtering** - Medium complexity, massive time savings
4. **Parallel Job Execution** - Medium-high complexity, competitive feature

### Phase 3: Polish (3-4 weeks)
5. **Interactive TUI** - High complexity, great UX improvement

---

## General Implementation Tips

### CLI Framework Patterns
```go
// Use cobra for command structure
var watchCmd = &cobra.Command{
    Use:   "watch",
    Short: "Watch for file changes and re-run tests",
    RunE:  watchExecute,
}

// Use viper for configuration
viper.SetConfigName(".testdrive")
viper.AddConfigPath(".")
viper.ReadInConfig()

// Use logrus for structured logging
log.WithFields(log.Fields{
    "job": "test",
    "duration": duration,
}).Info("Job completed")
```

### Testing Strategies
- Unit tests for core logic (pattern matching, graph algorithms)
- Integration tests for file watching, git operations
- Snapshot tests for CLI output
- Manual testing for TUI and hooks

### User Experience Principles
1. **Progressive Disclosure**: Start simple, reveal complexity as needed
2. **Fail Gracefully**: Always provide helpful error messages
3. **Show Progress**: Users should always know what's happening
4. **Make it Fast**: Performance is a feature
5. **Make it Configurable**: Provide sensible defaults, allow customization

### Documentation Requirements
For each feature, provide:
- Usage examples with common scenarios
- Configuration reference
- Troubleshooting guide
- Migration guide (if breaking changes)

---

## Success Metrics

Track these metrics to measure feature adoption:
- **Watch Mode**: % of users who use watch at least once per week
- **Parallel Execution**: Average time savings vs sequential
- **TUI**: % of users who use TUI vs CLI
- **File Filtering**: % of runs using --changed flag
- **Git Hooks**: % of projects with hooks installed

---

## Community Feedback

Create GitHub discussions for each feature:
- Gather use cases and requirements
- Solicit design feedback
- Beta test with early adopters
- Iterate based on real usage patterns

---

**Last Updated**: 2025-01-02
**Contributors**: testdrive core team
