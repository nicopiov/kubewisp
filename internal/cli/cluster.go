package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

func newClusterCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	command := &cobra.Command{
		Use:   "cluster",
		Short: "Connect to and inspect the current cluster",
	}
	command.AddCommand(newClusterStatusCommand(dependencies, configPath))
	return command
}

func newClusterStatusCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Verify connectivity to the current cluster",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return err
			}
			if cfg.CurrentProfile == "" {
				return errors.New("no current profile; run `kubewisp init` or `kubewisp profile use <name>`")
			}
			profile := cfg.Profiles[cfg.CurrentProfile]
			namespace := profile.CurrentNamespace
			if namespace == "" {
				namespace = profile.DefaultNamespace
			}
			if dependencies.Connectivity == nil {
				return errors.New("Kubernetes connectivity checker is not configured")
			}

			report, err := dependencies.Connectivity.Check(command.Context(), namespace)
			if err != nil {
				return fmt.Errorf("connect to profile %q: %w", cfg.CurrentProfile, err)
			}

			fmt.Fprintf(command.OutOrStdout(), "Profile: %s\n", cfg.CurrentProfile)
			fmt.Fprintf(command.OutOrStdout(), "Project: %s\n", profile.ProjectID)
			fmt.Fprintf(command.OutOrStdout(), "Cluster: %s\n", profile.ClusterName)
			fmt.Fprintf(command.OutOrStdout(), "Namespace: %s\n", report.Namespace)
			fmt.Fprintf(command.OutOrStdout(), "Kubernetes: %s\n", report.ServerVersion)
			fmt.Fprintln(command.OutOrStdout(), "Status: connected")
			return nil
		},
	}
}
