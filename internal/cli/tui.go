package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/gcloud"
	"github.com/nicopiov/kubewisp/internal/selector"
	"github.com/spf13/cobra"
)

func runTUICommand(dependencies Dependencies, configPath *string) func(*cobra.Command, []string) error {
	return func(command *cobra.Command, _ []string) error {
		if dependencies.TUI == nil {
			return errors.New("TUI dashboard is not configured")
		}
		if dependencies.Runner != nil && dependencies.Connectivity != nil && dependencies.Selector != nil {
			if err := prepareTUIProfile(command, dependencies, *configPath); err != nil {
				return err
			}
		}
		return dependencies.TUI.Run(
			command.Context(),
			command.InOrStdin(),
			command.OutOrStdout(),
			*configPath,
		)
	}
}

func prepareTUIProfile(command *cobra.Command, dependencies Dependencies, configPath string) error {
	output := command.OutOrStdout()
	fmt.Fprintln(output, "Preparing Kubewisp...")
	fmt.Fprintln(output, "  Loading saved profiles...")
	store := config.Store{Path: configPath}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	profileName, err := selectStartupProfile(command, dependencies, cfg)
	if err != nil {
		if errors.Is(err, selector.ErrCancelled) {
			return errors.New("profile selection cancelled")
		}
		return err
	}
	if profileName != cfg.CurrentProfile {
		fmt.Fprintf(output, "  Saving selected profile %q...\n", profileName)
		cfg.CurrentProfile = profileName
		if err := store.Save(cfg); err != nil {
			return err
		}
	}
	profile := cfg.Profiles[profileName]
	fmt.Fprintf(output, "  Using profile %q (%s / %s)...\n", profileName, profile.ProjectID, profile.ClusterName)
	client := gcloud.NewClient(dependencies.Runner)
	if err := activateAndCheckProfile(command, dependencies, client, profile, output); err == nil {
		fmt.Fprintln(output, "  Opening dashboard...")
		return nil
	} else if !gcloud.IsReauthenticationError(err) {
		return err
	}

	fmt.Fprintln(output, "Your Google Cloud authentication has expired or was revoked.")
	reader := bufio.NewReader(command.InOrStdin())
	if _, err := reauthenticateGcloud(command, reader, output, client); err != nil {
		return err
	}
	if err := activateAndCheckProfile(command, dependencies, client, profile, output); err != nil {
		return fmt.Errorf("connect profile %q after reauthentication: %w", profileName, err)
	}
	fmt.Fprintf(output, "Reauthenticated and connected profile %q.\n", profileName)
	fmt.Fprintln(output, "  Opening dashboard...")
	return nil
}

func selectStartupProfile(command *cobra.Command, dependencies Dependencies, cfg config.Config) (string, error) {
	if len(cfg.Profiles) == 0 {
		return "", errors.New("no profiles configured; run `kubewisp init`")
	}
	if len(cfg.Profiles) == 1 {
		for name := range cfg.Profiles {
			return name, nil
		}
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return dependencies.Selector.Select(
		command.Context(),
		command.InOrStdin(),
		command.OutOrStdout(),
		"Select a Kubewisp profile",
		names,
		cfg.CurrentProfile,
	)
}

func activateAndCheckProfile(
	command *cobra.Command,
	dependencies Dependencies,
	client *gcloud.Client,
	profile config.Profile,
	output io.Writer,
) error {
	fmt.Fprintf(output, "  Activating Google Cloud project %s...\n", profile.ProjectID)
	if err := client.SetProject(command.Context(), profile.ProjectID); err != nil {
		return err
	}
	fmt.Fprintf(output, "  Refreshing GKE credentials for %s...\n", profile.ClusterName)
	if err := client.GetCredentials(command.Context(), profile.ProjectID, gcloud.ClusterFromProfile(profile)); err != nil {
		return err
	}
	namespace := profile.CurrentNamespace
	if namespace == "" {
		namespace = profile.DefaultNamespace
	}
	fmt.Fprintf(output, "  Verifying Kubernetes API access to namespace %s...\n", namespace)
	if _, err := dependencies.Connectivity.Check(command.Context(), namespace); err != nil {
		return err
	}
	fmt.Fprintln(output, "  Cluster connection ready.")
	return nil
}

func newTUICommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open the interactive dashboard",
		Long: `Prepare the current profile and open the interactive dashboard.

Startup activates the saved Google Cloud project, refreshes GKE credentials,
verifies Kubernetes access, and offers reauthentication when needed.`,
		Example: "  kubewisp tui",
		Args:    cobra.NoArgs,
		RunE:    runTUICommand(dependencies, configPath),
	}
}
