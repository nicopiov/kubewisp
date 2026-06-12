package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/spf13/cobra"
)

func newProfileCommand(configPath *string) *cobra.Command {
	command := &cobra.Command{
		Use:   "profile",
		Short: "Manage cluster profiles",
	}

	command.AddCommand(
		newProfileListCommand(configPath),
		newProfileShowCommand(configPath),
		newProfileUseCommand(configPath),
	)
	return command
}

func newProfileListCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured profiles",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return err
			}

			names := make([]string, 0, len(cfg.Profiles))
			for name := range cfg.Profiles {
				names = append(names, name)
			}
			sort.Strings(names)

			if len(names) == 0 {
				fmt.Fprintln(command.OutOrStdout(), "No profiles configured.")
				return nil
			}

			for _, name := range names {
				marker := " "
				if name == cfg.CurrentProfile {
					marker = "*"
				}
				profile := cfg.Profiles[name]
				namespace := profile.CurrentNamespace
				if namespace == "" {
					namespace = profile.DefaultNamespace
				}
				fmt.Fprintf(
					command.OutOrStdout(),
					"%s %-20s %s / %s / %s\n",
					marker,
					name,
					profile.ProjectID,
					profile.ClusterName,
					namespace,
				)
			}
			return nil
		},
	}
}

func newProfileShowCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show [name]",
		Short: "Show a configured profile",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return err
			}

			name := cfg.CurrentProfile
			if len(args) == 1 {
				name = args[0]
			}
			if name == "" {
				return fmt.Errorf("no current profile; provide a profile name")
			}

			profile, ok := cfg.Profiles[name]
			if !ok {
				return fmt.Errorf("profile %q does not exist", name)
			}
			namespace := profile.CurrentNamespace
			if namespace == "" {
				namespace = profile.DefaultNamespace
			}

			fmt.Fprintf(command.OutOrStdout(), "Name: %s\n", name)
			fmt.Fprintf(command.OutOrStdout(), "Provider: %s\n", profile.Provider)
			fmt.Fprintf(command.OutOrStdout(), "Project: %s\n", profile.ProjectID)
			fmt.Fprintf(command.OutOrStdout(), "Cluster: %s\n", profile.ClusterName)
			fmt.Fprintf(command.OutOrStdout(), "Location: %s (%s)\n", profile.Location, profile.LocationType)
			fmt.Fprintf(command.OutOrStdout(), "Namespace: %s\n", namespace)
			fmt.Fprintf(command.OutOrStdout(), "Production: %t\n", profile.Production)
			return nil
		},
	}
}

func newProfileUseCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the current profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return err
			}

			name := args[0]
			if _, ok := cfg.Profiles[name]; !ok {
				return fmt.Errorf("profile %q does not exist", name)
			}
			cfg.CurrentProfile = name

			if err := (config.Store{Path: *configPath}).Save(cfg); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Current profile set to %q.\n", name)
			return nil
		},
	}
}

func loadConfig(path string) (config.Config, error) {
	cfg, err := (config.Store{Path: path}).Load()
	if errors.Is(err, os.ErrNotExist) {
		return config.Config{}, errors.New("kubewisp is not initialized; run `kubewisp init`")
	}
	return cfg, err
}
