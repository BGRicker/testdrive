package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config captures CLI options sourced from config files or flags.
type Config struct {
	Provider  string   `yaml:"provider"`
	Workflows []string `yaml:"workflows"`
	Jobs      []string `yaml:"jobs"`

	OnlySteps   []string `yaml:"only_step"`
	SkipSteps   []string `yaml:"skip_step"`
	UseLocalEnv bool     `yaml:"use_local_env"`

	DryRun  bool   `yaml:"dry_run"`
	Verbose bool   `yaml:"verbose"`
	Format  string `yaml:"format"`

	AutoFix      bool          `yaml:"auto_fix"`
	AutoFixRules []AutoFixRule `yaml:"auto_fix_rules"`

	Watch WatchConfig `yaml:"watch"`

	SmartFilter      bool              `yaml:"smart_filter"`
	SmartFilterRules []SmartFilterRule `yaml:"smart_filter_rules"`

	Warn                      WarnConfig `yaml:"warn"`
	PrivilegedCommandPatterns []string   `yaml:"privileged_command_patterns"`
}

// WatchConfig controls file watching behavior.
type WatchConfig struct {
	DebounceMS      int      `yaml:"debounce_ms"`
	ClearOnRun      bool     `yaml:"clear_on_run"`
	IgnorePatterns  []string `yaml:"ignore_patterns"`
	IncludePatterns []string `yaml:"include_patterns"`
}

// SmartFilterRule defines how to map source files to test files.
type SmartFilterRule struct {
	Pattern     string   `yaml:"pattern"`      // Pattern to match source files (e.g., "app/models/**/*.rb")
	TestPattern string   `yaml:"test_pattern"` // Pattern for test files (e.g., "spec/models/**/*_spec.rb")
	Additional  []string `yaml:"additional"`   // Additional patterns to include (e.g., integration tests)
}

// AutoFixRule defines how to transform a command for auto-fixing.
type AutoFixRule struct {
	Pattern     string   `yaml:"pattern"`      // Pattern to match in command (uses word boundaries)
	RemoveFlags []string `yaml:"remove_flags"` // Flags to remove (e.g., "--parallel", "--check")
	AddFlags    []string `yaml:"add_flags"`    // Flags to add (e.g., "-A", "--fix")
	Replace     string   `yaml:"replace"`      // If set, replaces entire command (ignores flag operations)
}

// WarnConfig controls additional warning behaviour.
type WarnConfig struct {
	VersionMismatch bool `yaml:"version_mismatch"`
}

// Default returns the baseline configuration used when no flags or config file specify values.
func Default() Config {
	return Config{
		Provider:         ProviderAuto,
		Format:           FormatPretty,
		AutoFixRules:     DefaultAutoFixRules(),
		SmartFilterRules: DefaultSmartFilterRules(),
		Watch: WatchConfig{
			DebounceMS: 300,
			ClearOnRun: true,
		},
		Warn: WarnConfig{
			VersionMismatch: true,
		},
	}
}

// DefaultAutoFixRules returns sensible auto-fix transformations for common linting tools.
// More specific patterns should come first to avoid partial matches.
func DefaultAutoFixRules() []AutoFixRule {
	return []AutoFixRule{
		{
			Pattern:     "rubocop",
			RemoveFlags: []string{"--parallel"},
			AddFlags:    []string{"-A"},
		},
		{
			// Rails standard task - must come before generic "standard" rule
			Pattern: "bin/rails standard",
			Replace: "bin/rails standard:fix",
		},
		{
			// More specific pattern first to avoid matching "standard"
			Pattern:  "standardrb",
			AddFlags: []string{"--fix"},
		},
		{
			Pattern:  "standard",
			AddFlags: []string{"--fix"},
		},
		{
			Pattern:     "prettier",
			RemoveFlags: []string{"--check"},
			AddFlags:    []string{"--write"},
		},
		{
			Pattern:  "eslint",
			AddFlags: []string{"--fix"},
		},
		{
			Pattern:  "ruff check",
			AddFlags: []string{"--fix"},
		},
		{
			// Black auto-formats by default, but --check mode only checks
			Pattern:     "black",
			RemoveFlags: []string{"--check"},
		},
	}
}

// DefaultSmartFilterRules returns sensible file-to-test mappings for common frameworks.
func DefaultSmartFilterRules() []SmartFilterRule {
	return []SmartFilterRule{
		// Rails: Models
		{
			Pattern:     "app/models/**/*.rb",
			TestPattern: "spec/models/**/*_spec.rb",
		},
		// Rails: Controllers
		{
			Pattern:     "app/controllers/**/*.rb",
			TestPattern: "spec/controllers/**/*_spec.rb",
			Additional:  []string{"spec/requests/**/*_spec.rb", "spec/integration/**/*_spec.rb"},
		},
		// Rails: Views/Helpers
		{
			Pattern:     "app/views/**/*",
			TestPattern: "spec/views/**/*_spec.rb",
			Additional:  []string{"spec/features/**/*_spec.rb", "spec/system/**/*_spec.rb"},
		},
		{
			Pattern:     "app/helpers/**/*.rb",
			TestPattern: "spec/helpers/**/*_spec.rb",
		},
		// Rails: Jobs/Mailers/Channels
		{
			Pattern:     "app/jobs/**/*.rb",
			TestPattern: "spec/jobs/**/*_spec.rb",
		},
		{
			Pattern:     "app/mailers/**/*.rb",
			TestPattern: "spec/mailers/**/*_spec.rb",
		},
		{
			Pattern:     "app/channels/**/*.rb",
			TestPattern: "spec/channels/**/*_spec.rb",
		},
		// Go: Source files
		{
			Pattern:     "**/*.go",
			TestPattern: "**/*_test.go",
		},
		// JavaScript/TypeScript: Source files
		{
			Pattern:     "src/**/*.js",
			TestPattern: "**/*.test.js",
			Additional:  []string{"**/*.spec.js"},
		},
		{
			Pattern:     "src/**/*.ts",
			TestPattern: "**/*.test.ts",
			Additional:  []string{"**/*.spec.ts"},
		},
		{
			Pattern:     "src/**/*.jsx",
			TestPattern: "**/*.test.jsx",
			Additional:  []string{"**/*.spec.jsx"},
		},
		{
			Pattern:     "src/**/*.tsx",
			TestPattern: "**/*.test.tsx",
			Additional:  []string{"**/*.spec.tsx"},
		},
		// Python
		{
			Pattern:     "**/*.py",
			TestPattern: "**/test_*.py",
			Additional:  []string{"**/*_test.py"},
		},
	}
}

const (
	// ProviderAuto selects the provider based on repository contents.
	ProviderAuto = "auto"
	// ProviderGitHub forces GitHub Actions provider.
	ProviderGitHub = "github"

	// FormatPretty renders human readable output.
	FormatPretty = "pretty"
	// FormatJSON renders machine readable output.
	FormatJSON = "json"
)

// Load reads .testdrive.yml from the repository root when present. Missing files are ignored.
func Load(root string) (Config, error) {
	cfg := Default()
	path := filepath.Join(root, ".testdrive.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config %q: %w", path, err)
	}

	var fileCfg Config
	if err := yaml.Unmarshal(data, &fileCfg); err != nil {
		return cfg, fmt.Errorf("parse config %q: %w", path, err)
	}

	meta := parseConfigMeta(data)
	cfg = merge(cfg, fileCfg, meta)
	return cfg, nil
}

type configMeta struct {
	watchFields map[string]bool
}

func parseConfigMeta(data []byte) configMeta {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return configMeta{}
	}

	watchRaw, ok := raw["watch"].(map[string]any)
	if !ok {
		return configMeta{}
	}

	fields := make(map[string]bool, len(watchRaw))
	for key := range watchRaw {
		fields[key] = true
	}

	return configMeta{watchFields: fields}
}

func merge(base, override Config, meta configMeta) Config {
	out := base

	if override.Provider != "" {
		out.Provider = override.Provider
	}
	if len(override.Workflows) > 0 {
		out.Workflows = append([]string{}, override.Workflows...)
	}
	if len(override.Jobs) > 0 {
		out.Jobs = append([]string{}, override.Jobs...)
	}
	if len(override.OnlySteps) > 0 {
		out.OnlySteps = append([]string{}, override.OnlySteps...)
	}
	if len(override.SkipSteps) > 0 {
		out.SkipSteps = append([]string{}, override.SkipSteps...)
	}
	if len(override.PrivilegedCommandPatterns) > 0 {
		out.PrivilegedCommandPatterns = append([]string{}, override.PrivilegedCommandPatterns...)
	}
	if override.Format != "" {
		out.Format = override.Format
	}
	if override.DryRun {
		out.DryRun = true
	}
	if override.Verbose {
		out.Verbose = true
	}
	if override.UseLocalEnv {
		out.UseLocalEnv = true
	}
	if override.AutoFix {
		out.AutoFix = true
	}
	if override.AutoFixRules != nil {
		// User-provided rules completely replace defaults (even if empty list)
		out.AutoFixRules = append([]AutoFixRule{}, override.AutoFixRules...)
	}
	if override.SmartFilter {
		out.SmartFilter = true
	}
	if override.SmartFilterRules != nil {
		// User-provided rules completely replace defaults (even if empty list)
		out.SmartFilterRules = append([]SmartFilterRule{}, override.SmartFilterRules...)
	}

	if len(meta.watchFields) > 0 {
		if meta.watchFields["debounce_ms"] {
			out.Watch.DebounceMS = override.Watch.DebounceMS
		}
		if meta.watchFields["clear_on_run"] {
			out.Watch.ClearOnRun = override.Watch.ClearOnRun
		}
		if meta.watchFields["ignore_patterns"] {
			out.Watch.IgnorePatterns = append([]string{}, override.Watch.IgnorePatterns...)
		}
		if meta.watchFields["include_patterns"] {
			out.Watch.IncludePatterns = append([]string{}, override.Watch.IncludePatterns...)
		}
	}

	if override.Warn.VersionMismatch {
		out.Warn.VersionMismatch = true
	}

	return out
}

// ApplyFlags mutates cfg by applying values from CLI flags when they are present.
func ApplyFlags(cfg *Config, flags FlagValues) {
	if flags.Provider.Set {
		cfg.Provider = flags.Provider.Value
	}
	if len(flags.Workflows.Values) > 0 {
		cfg.Workflows = append([]string{}, flags.Workflows.Values...)
	}
	if len(flags.Jobs.Values) > 0 {
		cfg.Jobs = append([]string{}, flags.Jobs.Values...)
	}
	if len(flags.OnlySteps.Values) > 0 {
		cfg.OnlySteps = append([]string{}, flags.OnlySteps.Values...)
	}
	if len(flags.SkipSteps.Values) > 0 {
		cfg.SkipSteps = append([]string{}, flags.SkipSteps.Values...)
	}
	if flags.Format.Set {
		cfg.Format = flags.Format.Value
	}
	if flags.DryRun.Set {
		cfg.DryRun = flags.DryRun.Value
	}
	if flags.Verbose.Set {
		cfg.Verbose = flags.Verbose.Value
	}
	if flags.UseLocalEnv.Set {
		cfg.UseLocalEnv = flags.UseLocalEnv.Value
	}
	if flags.AutoFix.Set {
		cfg.AutoFix = flags.AutoFix.Value
	}
	if flags.SmartFilter.Set {
		cfg.SmartFilter = flags.SmartFilter.Value
	}
}

// FlagValues captures CLI flag state with knowledge of whether each flag was set explicitly.
type FlagValues struct {
	Provider    StringFlag
	Workflows   SliceFlag
	Jobs        SliceFlag
	OnlySteps   SliceFlag
	SkipSteps   SliceFlag
	Format      StringFlag
	DryRun      BoolFlag
	Verbose     BoolFlag
	UseLocalEnv BoolFlag
	AutoFix     BoolFlag
	SmartFilter BoolFlag
}

// StringFlag represents a string flag and whether it was set.
type StringFlag struct {
	Value string
	Set   bool
}

// SliceFlag represents a slice flag and whether it captured values via CLI.
type SliceFlag struct {
	Values []string
}

// BoolFlag represents a bool flag and whether it was set.
type BoolFlag struct {
	Value bool
	Set   bool
}
