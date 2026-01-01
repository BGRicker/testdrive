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

	AutoFix      bool           `yaml:"auto_fix"`
	AutoFixRules []AutoFixRule  `yaml:"auto_fix_rules"`

	Warn                      WarnConfig `yaml:"warn"`
	PrivilegedCommandPatterns []string   `yaml:"privileged_command_patterns"`
}

// AutoFixRule defines how to transform a command for auto-fixing.
type AutoFixRule struct {
	Pattern     string   `yaml:"pattern"`      // Substring or regex to match in command
	RemoveFlags []string `yaml:"remove_flags"` // Flags to remove (e.g., "--parallel", "--check")
	AddFlags    []string `yaml:"add_flags"`    // Flags to add (e.g., "-A", "--fix")
	Replace     string   `yaml:"replace"`      // If set, replaces entire command
}

// WarnConfig controls additional warning behaviour.
type WarnConfig struct {
	VersionMismatch bool `yaml:"version_mismatch"`
}

// Default returns the baseline configuration used when no flags or config file specify values.
func Default() Config {
	return Config{
		Provider:     ProviderAuto,
		Format:       FormatPretty,
		AutoFixRules: DefaultAutoFixRules(),
		Warn: WarnConfig{
			VersionMismatch: true,
		},
	}
}

// DefaultAutoFixRules returns sensible auto-fix transformations for common linting tools.
func DefaultAutoFixRules() []AutoFixRule {
	return []AutoFixRule{
		{
			Pattern:     "rubocop",
			RemoveFlags: []string{"--parallel"},
			AddFlags:    []string{"-A"},
		},
		{
			Pattern:  "standard",
			AddFlags: []string{"--fix"},
		},
		{
			Pattern:  "standardrb",
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
			Pattern:  "black",
			AddFlags: []string{},
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

	cfg = merge(cfg, fileCfg)
	return cfg, nil
}

func merge(base, override Config) Config {
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
	if len(override.AutoFixRules) > 0 {
		// User-provided rules completely replace defaults
		out.AutoFixRules = append([]AutoFixRule{}, override.AutoFixRules...)
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
