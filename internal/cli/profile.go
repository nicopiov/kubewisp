package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/spf13/cobra"
)

func newProfileCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	command := &cobra.Command{
		Use:   "profile",
		Short: "Manage cluster profiles",
		Long: `Create and manage Kubewisp profiles.

Profiles store cluster selection and safety preferences only. Google
credentials remain managed by gcloud.`,
		Example: `  kubewisp profile list
  kubewisp profile add
  kubewisp profile use production
  kubewisp profile rename staging development
  kubewisp profile delete old-cluster`,
		Args: cobra.NoArgs,
	}

	command.AddCommand(
		newProfileAddCommand(dependencies, configPath),
		newProfileListCommand(configPath),
		newProfileShowCommand(configPath),
		newProfileUseCommand(configPath),
		newProfileRenameCommand(configPath),
		newProfileDeleteCommand(configPath),
	)
	return command
}

func newProfileRenameCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <old-name> <new-name>",
		Short: "Rename a configured profile",
		Args:  cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return err
			}
			oldName, newName := args[0], args[1]
			profile, exists := cfg.Profiles[oldName]
			if !exists {
				return fmt.Errorf("profile %q does not exist", oldName)
			}
			if _, exists := cfg.Profiles[newName]; exists {
				return fmt.Errorf("profile %q already exists", newName)
			}
			delete(cfg.Profiles, oldName)
			cfg.Profiles[newName] = profile
			if cfg.CurrentProfile == oldName {
				cfg.CurrentProfile = newName
			}
			if err := (config.Store{Path: *configPath}).Save(cfg); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Profile %q renamed to %q.\n", oldName, newName)
			return nil
		},
	}
}

func newProfileDeleteCommand(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a configured profile after confirmation",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			cfg, err := loadConfig(*configPath)
			if err != nil {
				return err
			}
			name := args[0]
			if _, exists := cfg.Profiles[name]; !exists {
				return fmt.Errorf("profile %q does not exist", name)
			}
			confirmed, err := promptYesNo(
				bufio.NewReader(command.InOrStdin()),
				command.OutOrStdout(),
				fmt.Sprintf("Delete profile %q?", name),
				false,
			)
			if err != nil {
				return err
			}
			if !confirmed {
				fmt.Fprintln(command.OutOrStdout(), "Profile deletion cancelled.")
				return nil
			}
			delete(cfg.Profiles, name)
			if cfg.CurrentProfile == name {
				cfg.CurrentProfile = firstProfileName(cfg.Profiles)
			}
			if err := (config.Store{Path: *configPath}).Save(cfg); err != nil {
				return err
			}
			fmt.Fprintf(command.OutOrStdout(), "Profile %q deleted.\n", name)
			return nil
		},
	}
}

func firstProfileName(profiles map[string]config.Profile) string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	return names[0]
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
