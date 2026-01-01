# Testdrive

Testdrive mirrors your GitHub Actions `run:` steps locally so you can catch failures before pushing. It discovers workflows, respects job and step filters, and outputs either a concise pretty report or machine-friendly JSON.

## Install

```bash
go install github.com/bgricker/testdrive/cmd/testdrive@latest
```

## Usage

```bash
# List the jobs and steps that would run
$ testdrive list

# Execute all run: steps sequentially
$ testdrive run

# Preview commands without executing
$ testdrive run --dry-run

# Filter by job/steps and switch formats
$ testdrive run --job test --only-step "Lint" --format json

# Stream command output as it runs
$ testdrive run --verbose

# Use local environment only, ignoring workflow env variables
$ testdrive run --use-local-env

# Skip specific steps (e.g., database setup when you already have a working DB)
$ testdrive run --skip-step "Set up database"

# Transform lint commands to auto-fix mode
$ testdrive run --auto-fix

# Allow privileged commands (e.g., sudo/apt-get) when absolutely necessary
$ TESTDRIVE_ALLOW_PRIVILEGED=1 testdrive run
```

### Streaming UI (GitHub-style)

When format is `pretty` (default) and not in verbose mode, Testdrive renders a live, GitHub-style summary:

- ✅/❌ per job with individual timers
- 🟢 while a job is running, ⏳ when queued
- Failed jobs expand to show step breakdown, durations, the exact `Command:` run, and cleaned failure output (including parsed RSpec failures)
- Routine CI noise is suppressed in streaming mode to keep output focused

Example:

```
✅ lint (3.2s)
✅ scan_js (1.9s)
❌ test (31.4s)
    ⏭️ Install packages (0s)
    ✅ Install modules (247ms)
    ❌ Run tests (31.1s)
      Command: bin/rails db:setup spec
      spec/jobs/foo_spec.rb:123 expected X got Y
```

Flags such as `--workflow`, `--job`, `--only-step`, and `--skip-step` accept multiple values and support substring or `/regex/` matches. When no workflows are provided, Testdrive automatically loads `.github/workflows/*.yml`/`*.yaml` in lexicographic order. Execution stops with a non-zero exit code if any step fails, but all remaining steps continue to run so you see the full picture.

## Environment Support

Testdrive automatically inherits your shell environment and supports version managers:

- **asdf**: Automatically sources `asdf.sh` (or `asdf.fish` for fish shell) to ensure correct Ruby, Node, Python versions
- **rbenv**: Works with your existing rbenv setup
- **Shell compatibility**: Supports bash, zsh, ksh, sh, and fish shells
- **Environment variables**: Merges workflow → job → step environment variables (override with `--use-local-env`)
- **Working directories**: Respects `working-directory` settings from workflows

### Local Environment Mode

Testdrive intelligently handles environment variables to make local testing seamless:

**Smart Auto-Detection:**
When your workflow defines `services:` (like PostgreSQL, Redis, etc.), testdrive **automatically uses your local environment** instead of workflow env variables. This happens because testdrive can't run services containers, so the workflow's env vars (pointing to non-existent services) won't work anyway.

```bash
# If your workflow has services:, local env is used automatically!
$ testdrive run
# Warning shown: "services are not supported; automatically using local environment instead"
```

**Manual Override:**
You can explicitly control environment behavior:

```bash
# Force local env (even without services)
$ testdrive run --use-local-env

# Or configure it permanently in .testdrive.yml:
use_local_env: true
```

This is particularly useful when:
- Your workflow defines CI-specific database connections (like `postgres:postgres@localhost`)
- You have local credentials in `.env` files or shell environment
- You want consistent behavior across all workflows

### Auto-Fix Mode

When developing locally, you often want linters to automatically fix issues rather than just report them. Use `--auto-fix` to transform lint commands to their auto-fix variants:

```bash
# Automatically transforms common linting tools to fix mode
$ testdrive run --auto-fix
```

**Built-in transformations:**

| Original Command | Transformed Command |
|-----------------|---------------------|
| `rubocop --parallel` | `rubocop -A` |
| `bin/rails standard` | `bin/rails standard:fix` |
| `standard` | `standard --fix` |
| `standardrb` | `standardrb --fix` |
| `prettier --check` | `prettier --write` |
| `eslint` | `eslint --fix` |
| `ruff check` | `ruff check --fix` |
| `black --check` | `black` |

**Custom transformations** can be configured in `.testdrive.yml`:

```yaml
auto_fix: true  # Enable by default
auto_fix_rules:
  - pattern: 'yarn lint'
    replace: 'yarn fix:prettier'
  - pattern: 'custom-linter'
    remove_flags: ['--strict']
    add_flags: ['--fix']
```

## Configuration

An optional `.testdrive.yml` can provide defaults for the CLI. Command-line flags always win over config values.

```yaml
provider: github          # auto|github (defaults to auto)
workflows:
  - .github/workflows/ci.yml
jobs:
  - test
only_step:
  - /lint/
skip_step:
  - "Upload artifact"
  - "Set up database schema"  # Skip DB setup if you already have a working database
use_local_env: false      # Set to true to ignore workflow env variables
auto_fix: false           # Set to true to transform lint commands to auto-fix mode
auto_fix_rules:           # Custom auto-fix transformations (replaces defaults if provided)
  - pattern: 'yarn lint'
    replace: 'yarn fix:prettier'
dry_run: false
verbose: false
format: pretty             # pretty|json
warn:
  version_mismatch: true   # warn when local Ruby/Node major.minor differs
privileged_command_patterns:
  - (?i)^sudo\b
  - (?i)\bapt-get\b
```

## Current Status

- ✅ GitHub Actions workflow parser (run steps only)
- ✅ Sequential execution with env/shell/working-directory resolution
- ✅ Pretty & JSON reporters; streaming GitHub-style UI with live timers
- ✅ Dry-run, verbose streaming, job/step filters, repeatable `--workflow`
- ✅ Environment inheritance with asdf/rbenv support
- ✅ Cross-shell compatibility (bash, zsh, ksh, sh, fish)
- ✅ Privileged command detection and skipping
- 🚧 Upcoming: richer runtime pre-flight checks, additional CI providers, matrix & services support
  - Version mismatch warnings are enabled by default; set `warn.version_mismatch: false` to silence them.

Want to dig in? Run `go test ./...` to exercise the parser, runner, and CLI tests.
