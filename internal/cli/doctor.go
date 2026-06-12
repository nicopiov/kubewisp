package cli

import (
	"github.com/nicopiov/kubewisp/internal/doctor"
	"github.com/spf13/cobra"
)

func newDoctorCommand(dependencies Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check local dependencies and connectivity",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			service := doctor.NewService(dependencies.Runner)
			report := service.Run(command.Context())
			doctor.WriteReport(command.OutOrStdout(), report)

			if !report.Healthy() {
				return ErrMissingDependencies
			}
			return nil
		},
	}
}
