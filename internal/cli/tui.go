package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func runTUICommand(dependencies Dependencies, configPath *string) func(*cobra.Command, []string) error {
	return func(command *cobra.Command, _ []string) error {
		if dependencies.TUI == nil {
			return errors.New("TUI dashboard is not configured")
		}
		return dependencies.TUI.Run(
			command.Context(),
			command.InOrStdin(),
			command.OutOrStdout(),
			*configPath,
		)
	}
}

func newTUICommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive dashboard",
		Args:  cobra.NoArgs,
		RunE:  runTUICommand(dependencies, configPath),
	}
}
