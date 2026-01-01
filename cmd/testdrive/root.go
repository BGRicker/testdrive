package main

import (
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
        Use:           "testdrive",
        Short:         "Testdrive executes GitHub Actions steps locally",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	persistent := cmd.PersistentFlags()
	persistent.String("provider", "", "workflow provider to use (auto|github)")
	persistent.StringArray("workflow", nil, "workflow file to include")
	persistent.StringArray("job", nil, "job filter (repeatable)")
	persistent.StringArray("only-step", nil, "include only matching steps")
	persistent.StringArray("skip-step", nil, "exclude matching steps")
	persistent.Bool("use-local-env", false, "ignore workflow env variables, use only local environment")
	persistent.Bool("dry-run", false, "print commands without executing them")
	persistent.BoolP("verbose", "v", false, "stream command output in real time")
	persistent.String("format", "pretty", "output format (pretty|json)")

	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newRunCmd())

	return cmd
}
