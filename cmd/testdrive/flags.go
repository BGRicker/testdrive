package main

import (
	"fmt"

    "github.com/bgricker/testdrive/internal/config"
	"github.com/spf13/cobra"
)

func gatherFlags(cmd *cobra.Command) (config.FlagValues, error) {
	flags := cmd.Flags()
	var values config.FlagValues

	if flags.Changed("provider") {
		v, err := flags.GetString("provider")
		if err != nil {
			return values, fmt.Errorf("parse --provider: %w", err)
		}
		values.Provider = config.StringFlag{Value: v, Set: true}
	}

	if flags.Changed("workflow") {
		v, err := flags.GetStringArray("workflow")
		if err != nil {
			return values, fmt.Errorf("parse --workflow: %w", err)
		}
		values.Workflows = config.SliceFlag{Values: append([]string{}, v...)}
	}

	if flags.Changed("job") {
		v, err := flags.GetStringArray("job")
		if err != nil {
			return values, fmt.Errorf("parse --job: %w", err)
		}
		values.Jobs = config.SliceFlag{Values: append([]string{}, v...)}
	}

	if flags.Changed("only-step") {
		v, err := flags.GetStringArray("only-step")
		if err != nil {
			return values, fmt.Errorf("parse --only-step: %w", err)
		}
		values.OnlySteps = config.SliceFlag{Values: append([]string{}, v...)}
	}

	if flags.Changed("skip-step") {
		v, err := flags.GetStringArray("skip-step")
		if err != nil {
			return values, fmt.Errorf("parse --skip-step: %w", err)
		}
		values.SkipSteps = config.SliceFlag{Values: append([]string{}, v...)}
	}

	if flags.Changed("format") {
		v, err := flags.GetString("format")
		if err != nil {
			return values, fmt.Errorf("parse --format: %w", err)
		}
		values.Format = config.StringFlag{Value: v, Set: true}
	}

	if flags.Changed("dry-run") {
		v, err := flags.GetBool("dry-run")
		if err != nil {
			return values, fmt.Errorf("parse --dry-run: %w", err)
		}
		values.DryRun = config.BoolFlag{Value: v, Set: true}
	}

	if flags.Changed("verbose") {
		v, err := flags.GetBool("verbose")
		if err != nil {
			return values, fmt.Errorf("parse --verbose: %w", err)
		}
		values.Verbose = config.BoolFlag{Value: v, Set: true}
	}

	if flags.Changed("use-local-env") {
		v, err := flags.GetBool("use-local-env")
		if err != nil {
			return values, fmt.Errorf("parse --use-local-env: %w", err)
		}
		values.UseLocalEnv = config.BoolFlag{Value: v, Set: true}
	}

	return values, nil
}
