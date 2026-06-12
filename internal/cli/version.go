package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand(version string) *cobra.Command {
	if version == "" {
		version = "dev"
	}
	return &cobra.Command{
		Use:   "version",
		Short: "Print the installed Kubewisp version",
		Args:  cobra.NoArgs,
		Run: func(command *cobra.Command, _ []string) {
			fmt.Fprintf(command.OutOrStdout(), "kubewisp %s\n", version)
		},
	}
}
