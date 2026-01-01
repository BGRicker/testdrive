package main

import (
	"fmt"
	"os"
	"strings"

    "github.com/bgricker/testdrive/internal/config"
    "github.com/bgricker/testdrive/internal/output"
    "github.com/bgricker/testdrive/internal/runner"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Execute workflow steps locally",
		RunE:  runExecute,
	}
}

func runExecute(cmd *cobra.Command, args []string) error {
	cfg, root, err := loadConfig(cmd)
	if err != nil {
		return err
	}

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
		// In streaming mode, the renderer already showed initial job lines; don't print this footer.
		if !runOpts.Streaming {
			fmt.Fprintln(cmd.OutOrStdout(), "No matching jobs or steps")
		}
		return nil
	}

	warnings := collapseWarnings(filtered.warnings)

	switch strings.ToLower(cfg.Format) {
	case config.FormatPretty:
		// Only use pretty renderer if not streaming
		if !runOpts.Streaming {
			renderer := output.NewPretty(cmd.OutOrStdout())
			if err := renderer.RenderResults(results, summary); err != nil {
				return err
			}
		}
		// Only show warnings for non-streaming mode
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
