package cli

import (
	"errors"
	"fmt"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/kubectl"
	"github.com/nicopiov/kubewisp/internal/runner"
	"github.com/nicopiov/kubewisp/internal/selector"
	"github.com/nicopiov/kubewisp/internal/tui"
	"github.com/spf13/cobra"
)

var ErrMissingDependencies = errors.New("required dependencies are missing")

type Dependencies struct {
	Runner       runner.CommandRunner
	Connectivity kube.ConnectivityChecker
	Namespaces   kube.NamespaceService
	Pods         kube.PodService
	PortForward  kubectl.PortForwarder
	Selector     selector.Service
	TUI          tui.Service
}

func NewRootCommand(dependencies Dependencies) *cobra.Command {
	defaultConfigPath, err := config.DefaultPath()
	if err != nil {
		defaultConfigPath = ""
	}
	configPath := defaultConfigPath

	command := &cobra.Command{
		Use:           "kubewisp",
		Short:         "Safely navigate and operate GKE clusters",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE:          runTUICommand(dependencies, &configPath),
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			if configPath == "" {
				return fmt.Errorf("determine config path: %w", err)
			}
			return nil
		},
	}
	command.PersistentFlags().StringVar(
		&configPath,
		"config",
		defaultConfigPath,
		"config file path (default $KUBEWISP_CONFIG or ~/.config/kubewisp/config.yaml)",
	)

	command.AddCommand(newDoctorCommand(dependencies))
	command.AddCommand(newInitCommand(dependencies, &configPath))
	command.AddCommand(newClusterCommand(dependencies, &configPath))
	command.AddCommand(newNamespaceCommand(dependencies, &configPath))
	command.AddCommand(newPodsCommand(dependencies, &configPath))
	command.AddCommand(newProfileCommand(&configPath))
	command.AddCommand(newTUICommand(dependencies, &configPath))

	return command
}
