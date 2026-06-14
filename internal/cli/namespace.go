package cli

import (
	"errors"
	"fmt"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/selector"
	"github.com/spf13/cobra"
)

func newNamespaceCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	command := &cobra.Command{
		Use:   "namespace",
		Short: "List and select Kubernetes namespaces",
		Example: `  kubewisp namespace list
  kubewisp namespace switch
  kubewisp namespace switch api`,
		Args: cobra.NoArgs,
	}
	command.AddCommand(
		newNamespaceListCommand(dependencies, configPath),
		newNamespaceSwitchCommand(dependencies, configPath),
	)
	return command
}

func newNamespaceListCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List accessible namespaces",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			_, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Namespaces == nil {
				return errors.New("Kubernetes namespace service is not configured")
			}

			names, err := dependencies.Namespaces.List(command.Context())
			if err != nil {
				return fmt.Errorf("list namespaces for profile %q: %w", profileName, err)
			}
			currentNamespace := profile.CurrentNamespace
			if currentNamespace == "" {
				currentNamespace = profile.DefaultNamespace
			}
			for _, name := range names {
				marker := " "
				if name == currentNamespace {
					marker = "*"
				}
				fmt.Fprintf(command.OutOrStdout(), "%s %s\n", marker, name)
			}
			return nil
		},
	}
}

func newNamespaceSwitchCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "switch [namespace]",
		Short: "Select a namespace for the current profile",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			cfg, profileName, profile, err := currentProfile(*configPath)
			if err != nil {
				return err
			}
			if dependencies.Namespaces == nil {
				return errors.New("Kubernetes namespace service is not configured")
			}

			namespace := ""
			if len(args) == 1 {
				namespace = args[0]
			} else {
				namespace, err = selectNamespace(command, dependencies, profileName, profile)
				if errors.Is(err, selector.ErrCancelled) {
					fmt.Fprintln(command.OutOrStdout(), "Namespace selection cancelled.")
					return nil
				}
				if err != nil {
					return err
				}
			}

			if err := dependencies.Namespaces.Exists(command.Context(), namespace); err != nil {
				return fmt.Errorf("select namespace for profile %q: %w", profileName, err)
			}

			profile.CurrentNamespace = namespace
			cfg.Profiles[profileName] = profile
			if err := (config.Store{Path: *configPath}).Save(cfg); err != nil {
				return err
			}

			fmt.Fprintf(command.OutOrStdout(), "Namespace for profile %q set to %q.\n", profileName, namespace)
			return nil
		},
	}
}

func selectNamespace(
	command *cobra.Command,
	dependencies Dependencies,
	profileName string,
	profile config.Profile,
) (string, error) {
	if dependencies.Selector == nil {
		return "", errors.New("interactive selector is not configured")
	}
	names, err := dependencies.Namespaces.List(command.Context())
	if err != nil {
		return "", fmt.Errorf("list namespaces for profile %q: %w", profileName, err)
	}
	currentNamespace := profile.CurrentNamespace
	if currentNamespace == "" {
		currentNamespace = profile.DefaultNamespace
	}
	return dependencies.Selector.Select(
		command.Context(),
		command.InOrStdin(),
		command.OutOrStdout(),
		fmt.Sprintf("Select namespace for %s", profileName),
		names,
		currentNamespace,
	)
}

func currentProfile(configPath string) (config.Config, string, config.Profile, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return config.Config{}, "", config.Profile{}, err
	}
	if cfg.CurrentProfile == "" {
		return config.Config{}, "", config.Profile{}, errors.New(
			"no current profile; run `kubewisp init` or `kubewisp profile use <name>`",
		)
	}
	return cfg, cfg.CurrentProfile, cfg.Profiles[cfg.CurrentProfile], nil
}
