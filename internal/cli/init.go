package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/doctor"
	"github.com/nicopiov/kubewisp/internal/gcloud"
	"github.com/spf13/cobra"
)

func newInitCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Authenticate, select a GKE cluster, and create a profile",
		Long: `Run the guided first-time setup.

Kubewisp checks local dependencies, authenticates with gcloud, discovers
accessible projects and GKE clusters, fetches cluster credentials, verifies
Kubernetes access, and saves a local profile.`,
		Example: "  kubewisp init",
		Args:    cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			dependencyReport := doctor.NewService(dependencies.Runner).Run(command.Context())
			if !dependencyReport.Healthy() {
				doctor.WriteReport(command.OutOrStdout(), dependencyReport)
				return errors.New("install missing dependencies and rerun `kubewisp init`")
			}

			reader := bufio.NewReader(command.InOrStdin())
			output := command.OutOrStdout()
			client := gcloud.NewClient(dependencies.Runner)

			fmt.Fprintln(output, "Kubewisp setup")
			fmt.Fprintln(output, "Kubewisp uses gcloud for authentication and never stores Google credentials.")

			account, err := client.ActiveAccount(command.Context())
			if err != nil {
				if !gcloud.IsReauthenticationError(err) {
					return err
				}
				fmt.Fprintln(output, "Your Google Cloud authentication has expired or was revoked.")
				account, err = reauthenticateGcloud(command, reader, output, client)
				if err != nil {
					return err
				}
			}
			if account == "" {
				login, err := promptYesNo(reader, output, "No active Google account. Run gcloud auth login?", true)
				if err != nil {
					return err
				}
				if !login {
					return errors.New("an active Google account is required")
				}
				if err := client.Login(
					command.Context(),
					reader,
					output,
					command.ErrOrStderr(),
				); err != nil {
					return err
				}
				account, err = client.ActiveAccount(command.Context())
				if err != nil {
					return err
				}
				if account == "" {
					return errors.New("gcloud login completed without an active Google account")
				}
			}
			fmt.Fprintf(output, "Active Google account: %s\n", account)

			projects, err := client.ListProjects(command.Context())
			if err != nil && gcloud.IsReauthenticationError(err) {
				fmt.Fprintln(output, "Your Google Cloud authentication has expired or was revoked.")
				account, err = reauthenticateGcloud(command, reader, output, client)
				if err == nil {
					fmt.Fprintf(output, "Active Google account: %s\n", account)
					projects, err = client.ListProjects(command.Context())
				}
			}
			if err != nil {
				return err
			}
			projectID, err := chooseProject(reader, output, projects)
			if err != nil {
				return err
			}

			clusters, err := client.ListClusters(command.Context(), projectID)
			if err != nil {
				return err
			}
			cluster, err := chooseCluster(reader, output, clusters)
			if err != nil {
				return err
			}

			profileName, err := promptText(reader, output, "Profile name", cluster.Name)
			if err != nil {
				return err
			}
			namespace, err := promptText(reader, output, "Default namespace", "default")
			if err != nil {
				return err
			}
			productionDefault := containsProductionHint(profileName) ||
				containsProductionHint(projectID) ||
				containsProductionHint(cluster.Name)
			production, err := promptYesNo(reader, output, "Production profile?", productionDefault)
			if err != nil {
				return err
			}

			store := config.Store{Path: *configPath}
			cfg, err := store.Load()
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return err
				}
				cfg = config.New()
			}

			if _, exists := cfg.Profiles[profileName]; exists {
				overwrite, err := promptYesNo(reader, output, fmt.Sprintf("Profile %q exists. Replace it?", profileName), false)
				if err != nil {
					return err
				}
				if !overwrite {
					return fmt.Errorf("profile %q was not changed", profileName)
				}
			}

			fmt.Fprintf(output, "Setting gcloud project to %s...\n", projectID)
			if err := client.SetProject(command.Context(), projectID); err != nil {
				return err
			}
			fmt.Fprintf(output, "Fetching credentials for %s...\n", cluster.Name)
			if err := client.GetCredentials(command.Context(), projectID, cluster); err != nil {
				return err
			}
			if dependencies.Connectivity == nil {
				return errors.New("Kubernetes connectivity checker is not configured")
			}
			fmt.Fprintf(output, "Verifying Kubernetes API and namespace %s...\n", namespace)
			report, err := dependencies.Connectivity.Check(command.Context(), namespace)
			if err != nil {
				return fmt.Errorf("verify Kubernetes connectivity: %w", err)
			}
			fmt.Fprintf(output, "Connected to Kubernetes %s.\n", report.ServerVersion)

			cfg.Profiles[profileName] = config.Profile{
				Provider:         config.ProviderGKE,
				ProjectID:        projectID,
				ClusterName:      cluster.Name,
				LocationType:     cluster.LocationType,
				Location:         cluster.Location,
				DefaultNamespace: namespace,
				ContextAlias:     cluster.Name,
				Production:       production,
			}
			cfg.CurrentProfile = profileName
			if err := store.Save(cfg); err != nil {
				return err
			}

			fmt.Fprintf(output, "Profile %q is ready and saved to %s.\n", profileName, *configPath)
			fmt.Fprintln(output, "Run `kubewisp profile show` to inspect it.")
			return nil
		},
	}
}

func newProfileAddCommand(dependencies Dependencies, configPath *string) *cobra.Command {
	command := newInitCommand(dependencies, configPath)
	command.Use = "add"
	command.Short = "Authenticate, select a GKE cluster, and add a profile"
	command.Example = "  kubewisp profile add"
	return command
}

func reauthenticateGcloud(
	command *cobra.Command,
	reader *bufio.Reader,
	output io.Writer,
	client *gcloud.Client,
) (string, error) {
	login, err := promptYesNo(reader, output, "Run gcloud auth login now?", true)
	if err != nil {
		return "", err
	}
	if !login {
		return "", errors.New("Google Cloud reauthentication is required; run `gcloud auth login` and retry")
	}
	fmt.Fprintln(output, "Opening Google OAuth login in your browser...")
	fmt.Fprintln(output, "Waiting for gcloud to finish authentication; no terminal input is required.")
	if err := client.Login(command.Context(), reader, output, command.ErrOrStderr()); err != nil {
		return "", err
	}
	fmt.Fprintln(output, "Google Cloud authentication completed.")
	account, err := client.ActiveAccount(command.Context())
	if err != nil {
		return "", err
	}
	if account == "" {
		return "", errors.New("gcloud login completed without an active Google account")
	}
	return account, nil
}

func chooseProject(reader *bufio.Reader, output io.Writer, projects []string) (string, error) {
	if len(projects) == 0 {
		return "", errors.New("no accessible Google Cloud projects found")
	}
	if len(projects) == 1 {
		fmt.Fprintf(output, "Using project: %s\n", projects[0])
		return projects[0], nil
	}

	fmt.Fprintln(output, "Select a Google Cloud project:")
	for index, project := range projects {
		fmt.Fprintf(output, "  %d. %s\n", index+1, project)
	}
	return promptSelection(reader, output, "Project", projects)
}

func chooseCluster(reader *bufio.Reader, output io.Writer, clusters []gcloud.Cluster) (gcloud.Cluster, error) {
	if len(clusters) == 0 {
		return gcloud.Cluster{}, errors.New("no accessible GKE clusters found in the selected project")
	}
	if len(clusters) == 1 {
		fmt.Fprintf(output, "Using cluster: %s (%s, %s)\n", clusters[0].Name, clusters[0].Location, clusters[0].LocationType)
		return clusters[0], nil
	}

	fmt.Fprintln(output, "Select a GKE cluster:")
	options := make([]string, 0, len(clusters))
	for index, cluster := range clusters {
		label := fmt.Sprintf("%s (%s, %s)", cluster.Name, cluster.Location, cluster.LocationType)
		options = append(options, label)
		fmt.Fprintf(output, "  %d. %s\n", index+1, label)
	}
	selected, err := promptSelection(reader, output, "Cluster", options)
	if err != nil {
		return gcloud.Cluster{}, err
	}
	for index, option := range options {
		if selected == option {
			return clusters[index], nil
		}
	}
	return gcloud.Cluster{}, errors.New("selected cluster was not found")
}

func promptSelection(reader *bufio.Reader, output io.Writer, label string, options []string) (string, error) {
	for {
		value, err := promptText(reader, output, label+" number", "")
		if err != nil {
			return "", err
		}
		index, err := strconv.Atoi(value)
		if err == nil && index >= 1 && index <= len(options) {
			return options[index-1], nil
		}
		fmt.Fprintf(output, "Enter a number from 1 to %d.\n", len(options))
	}
}

func promptText(reader *bufio.Reader, output io.Writer, label, defaultValue string) (string, error) {
	if defaultValue == "" {
		fmt.Fprintf(output, "%s: ", label)
	} else {
		fmt.Fprintf(output, "%s [%s]: ", label, defaultValue)
	}

	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read %s: %w", strings.ToLower(label), err)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultValue
	}
	if value == "" {
		return "", fmt.Errorf("%s is required", strings.ToLower(label))
	}
	return value, nil
}

func promptYesNo(reader *bufio.Reader, output io.Writer, label string, defaultValue bool) (bool, error) {
	defaultLabel := "y/N"
	if defaultValue {
		defaultLabel = "Y/n"
	}

	for {
		value, err := promptText(reader, output, fmt.Sprintf("%s [%s]", label, defaultLabel), "")
		if err != nil {
			if strings.Contains(err.Error(), "is required") {
				return defaultValue, nil
			}
			return false, err
		}

		switch strings.ToLower(value) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(output, "Enter yes or no.")
		}
	}
}

func containsProductionHint(value string) bool {
	value = strings.ToLower(value)
	return strings.Contains(value, "production") || strings.Contains(value, "prod")
}
