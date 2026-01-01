package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bgricker/testdrive/internal/config"
	"github.com/bgricker/testdrive/internal/output"
	"github.com/bgricker/testdrive/internal/runner"
	"github.com/bgricker/testdrive/internal/smartfilter"
	"github.com/bgricker/testdrive/internal/watcher"
	"github.com/spf13/cobra"
)

func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Watch for file changes and re-run workflows",
		Long: `Watch for file changes and automatically re-run workflows when changes are detected.

The watch command monitors your project files for changes and automatically re-runs
workflows when relevant files are modified. This is useful during development to
get immediate feedback on your changes.

File changes are debounced (default 300ms) to avoid excessive re-runs when multiple
files are saved quickly. The terminal can optionally be cleared between runs for a
clean view of each execution.`,
		RunE: watchExecute,
	}
}

func watchExecute(cmd *cobra.Command, args []string) error {
	cfg, root, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Fprintln(cmd.OutOrStdout(), "\nStopping watch mode...")
		cancel()
	}()

	// Run initial execution first (before starting watcher)
	fmt.Fprintf(cmd.OutOrStdout(), "Running initial execution...\n\n")
	if err := executeRun(cmd, cfg, root); err != nil {
		// Don't exit on first failure, continue watching
		fmt.Fprintf(cmd.ErrOrStderr(), "Initial run failed: %v\n", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n👀 Watching for changes in %s\n", root)
	fmt.Fprintf(cmd.OutOrStdout(), "Press Ctrl+C to stop\n\n")

	// Create a channel to trigger re-runs
	runChan := make(chan []string, 1)

	// Set up file watcher AFTER initial run to avoid detecting changes from initial execution
	debounceDelay := time.Duration(cfg.Watch.DebounceMS) * time.Millisecond
	if debounceDelay == 0 {
		debounceDelay = 300 * time.Millisecond
	}

	w, err := watcher.New(watcher.Options{
		Root:            root,
		DebounceDelay:   debounceDelay,
		IgnorePatterns:  cfg.Watch.IgnorePatterns,
		IncludePatterns: cfg.Watch.IncludePatterns,
		OnChange: func(paths []string) {
			runChan <- paths
		},
	})
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}

	if err := w.Start(ctx); err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}
	defer w.Stop()

	// Watch loop
	for {
		select {
		case <-ctx.Done():
			return nil

		case changedFiles := <-runChan:
			// Clear terminal if configured
			if cfg.Watch.ClearOnRun {
				clearTerminal(cmd)
			}

			// Show which files changed
			fmt.Fprintf(cmd.OutOrStdout(), "🔄 Files changed:\n")
			for _, file := range changedFiles {
				fmt.Fprintf(cmd.OutOrStdout(), "  • %s\n", file)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "\n")

			// Find related tests if smart filtering is enabled
			var relatedTests []string
			if cfg.SmartFilter {
				relatedTests, err = smartfilter.FindRelatedTests(root, changedFiles, cfg.SmartFilterRules)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to find related tests: %v\n", err)
				} else if len(relatedTests) > 0 {
					fmt.Fprintf(cmd.OutOrStdout(), "🎯 Running related tests:\n")
					for _, testFile := range relatedTests {
						fmt.Fprintf(cmd.OutOrStdout(), "  • %s\n", testFile)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "\n")
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "ℹ️  No related tests found for changed files\n\n")
				}
			}

			// Temporarily stop the watcher to prevent detecting changes made by the run itself
			if err := w.Stop(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to stop watcher: %v\n", err)
			}

			// Re-run workflows (with smart filtering applied if enabled)
			if err := executeRunWithFilter(cmd, cfg, root, changedFiles, relatedTests); err != nil {
				// Continue watching even if run fails
				fmt.Fprintf(cmd.ErrOrStderr(), "Run failed: %v\n", err)
			}

			// Restart the watcher after the run completes
			w, err = watcher.New(watcher.Options{
				Root:            root,
				DebounceDelay:   debounceDelay,
				IgnorePatterns:  cfg.Watch.IgnorePatterns,
				IncludePatterns: cfg.Watch.IncludePatterns,
				OnChange: func(paths []string) {
					runChan <- paths
				},
			})
			if err != nil {
				return fmt.Errorf("recreate watcher: %w", err)
			}

			if err := w.Start(ctx); err != nil {
				return fmt.Errorf("restart watcher: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\n👀 Watching for changes...\n\n")
		}
	}
}

// executeRun performs a single workflow execution (extracted from runExecute)
func executeRun(cmd *cobra.Command, cfg config.Config, root string) error {
	data, err := loadPipeline(root, cfg)
	if err != nil {
		return err
	}

	filtered, err := applyFilters(data, cfg)
	if err != nil {
		return err
	}

	allowPrivileged := os.Getenv("TESTDRIVE_ALLOW_PRIVILEGED") == "1"

	runOpts := runner.Options{
		Root:               root,
		Stdout:             cmd.OutOrStdout(),
		Stderr:             cmd.ErrOrStderr(),
		Verbose:            cfg.Verbose,
		DryRun:             cfg.DryRun,
		TailLines:          20,
		AllowPrivileged:    allowPrivileged,
		PrivilegedPatterns: append([]string{}, cfg.PrivilegedCommandPatterns...),
		SkipSteps:          append([]string{}, cfg.SkipSteps...),
		UseLocalEnv:        cfg.UseLocalEnv,
		AutoFix:            cfg.AutoFix,
		AutoFixRules:       append([]config.AutoFixRule{}, cfg.AutoFixRules...),
	}

	// Enable streaming for pretty format when not verbose and not dry-run
	if strings.ToLower(cfg.Format) == config.FormatPretty && !cfg.Verbose && !cfg.DryRun {
		runOpts.Streaming = true
		runOpts.StreamingRenderer = output.NewStreamingPretty(cmd.OutOrStdout())
	}

	execRunner := runner.New(runOpts)
	results, summary, err := execRunner.Run(filtered.workflows)
	if err != nil {
		return err
	}

	if summary.TotalSteps == 0 {
		if !runOpts.Streaming {
			fmt.Fprintln(cmd.OutOrStdout(), "No matching jobs or steps")
		}
		return nil
	}

	warnings := collapseWarnings(filtered.warnings)

	switch strings.ToLower(cfg.Format) {
	case config.FormatPretty:
		if !runOpts.Streaming {
			renderer := output.NewPretty(cmd.OutOrStdout())
			if err := renderer.RenderResults(results, summary); err != nil {
				return err
			}
		}
		if !runOpts.Streaming && len(warnings) > 0 {
			for _, msg := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", msg)
			}
		}
	case config.FormatJSON:
		jsonReport := output.Report{
			Provider:  filtered.provider,
			Workflows: filtered.workflows,
			Steps:     results,
			Summary:   summary,
			Warnings:  warnings,
		}
		renderer := output.NewJSON(cmd.OutOrStdout())
		if err := renderer.Render(jsonReport); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported format %q", cfg.Format)
	}

	if summary.ExitCode != 0 {
		return fmt.Errorf("one or more steps failed")
	}

	return nil
}

// executeRunWithFilter performs a workflow execution with smart filtering applied.
func executeRunWithFilter(cmd *cobra.Command, cfg config.Config, root string, changedFiles []string, relatedTests []string) error {
	data, err := loadPipeline(root, cfg)
	if err != nil {
		return err
	}

	filtered, err := applyFilters(data, cfg)
	if err != nil {
		return err
	}

	allowPrivileged := os.Getenv("TESTDRIVE_ALLOW_PRIVILEGED") == "1"

	runOpts := runner.Options{
		Root:               root,
		Stdout:             cmd.OutOrStdout(),
		Stderr:             cmd.ErrOrStderr(),
		Verbose:            cfg.Verbose,
		DryRun:             cfg.DryRun,
		TailLines:          20,
		AllowPrivileged:    allowPrivileged,
		PrivilegedPatterns: append([]string{}, cfg.PrivilegedCommandPatterns...),
		SkipSteps:          append([]string{}, cfg.SkipSteps...),
		UseLocalEnv:        cfg.UseLocalEnv,
		AutoFix:            cfg.AutoFix,
		AutoFixRules:       append([]config.AutoFixRule{}, cfg.AutoFixRules...),
		SmartFilter:        cfg.SmartFilter,
		SmartFilterFiles:   relatedTests,
		SmartFilterChanged: changedFiles,
	}

	// Enable streaming for pretty format when not verbose and not dry-run
	if strings.ToLower(cfg.Format) == config.FormatPretty && !cfg.Verbose && !cfg.DryRun {
		runOpts.Streaming = true
		runOpts.StreamingRenderer = output.NewStreamingPretty(cmd.OutOrStdout())
	}

	execRunner := runner.New(runOpts)
	results, summary, err := execRunner.Run(filtered.workflows)
	if err != nil {
		return err
	}

	if summary.TotalSteps == 0 {
		if !runOpts.Streaming {
			fmt.Fprintln(cmd.OutOrStdout(), "No matching jobs or steps")
		}
		return nil
	}

	warnings := collapseWarnings(filtered.warnings)

	switch strings.ToLower(cfg.Format) {
	case config.FormatPretty:
		if !runOpts.Streaming {
			renderer := output.NewPretty(cmd.OutOrStdout())
			if err := renderer.RenderResults(results, summary); err != nil {
				return err
			}
		}
		if !runOpts.Streaming && len(warnings) > 0 {
			for _, msg := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", msg)
			}
		}
	case config.FormatJSON:
		jsonReport := output.Report{
			Provider:  filtered.provider,
			Workflows: filtered.workflows,
			Steps:     results,
			Summary:   summary,
			Warnings:  warnings,
		}
		renderer := output.NewJSON(cmd.OutOrStdout())
		if err := renderer.Render(jsonReport); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported format %q", cfg.Format)
	}

	if summary.ExitCode != 0 {
		return fmt.Errorf("one or more steps failed")
	}

	return nil
}

// clearTerminal clears the terminal screen
func clearTerminal(cmd *cobra.Command) {
	// ANSI escape sequence to clear screen and move cursor to top
	fmt.Fprint(cmd.OutOrStdout(), "\033[H\033[2J")
}
