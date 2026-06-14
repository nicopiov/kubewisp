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
	Workloads    kube.WorkloadService
	Events       kube.EventService
	PortForward  kubectl.PortForwarder
	Exec         kubectl.Executor
	Selector     selector.Service
	TUI          tui.Service
	Version      string
}

func NewRootCommand(dependencies Dependencies) *cobra.Command {
	defaultConfigPath, err := config.DefaultPath()
	if err != nil {
		defaultConfigPath = ""
	}
	configPath := defaultConfigPath

	command := &cobra.Command{
		Use:   "kubewisp",
		Short: "Safely navigate and operate GKE clusters",
		Long: `Kubewisp is a keyboard-driven CLI and TUI for navigating GKE clusters,
inspecting workloads, troubleshooting pods, and performing guarded operations.

Run kubewisp without a subcommand to select a profile, refresh GKE credentials,
verify connectivity, and open the interactive dashboard.`,
		Example: `  kubewisp
  kubewisp init
  kubewisp profile add
  kubewisp pods describe
  kubewisp workloads restart Deployment/api`,
		SilenceErrors: true,
		SilenceUsage:  true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		Args: cobra.NoArgs,
		RunE: runTUICommand(dependencies, &configPath),
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

	command.AddGroup(
		&cobra.Group{ID: "start", Title: "Start and setup:"},
		&cobra.Group{ID: "inspect", Title: "Inspect and operate:"},
		&cobra.Group{ID: "manage", Title: "Manage Kubewisp:"},
	)
	addGroupedCommand(command, "start", newInitCommand(dependencies, &configPath))
	addGroupedCommand(command, "start", newTUICommand(dependencies, &configPath))
	addGroupedCommand(command, "inspect", newClusterCommand(dependencies, &configPath))
	addGroupedCommand(command, "inspect", newNamespaceCommand(dependencies, &configPath))
	addGroupedCommand(command, "inspect", newPodsCommand(dependencies, &configPath))
	addGroupedCommand(command, "inspect", newWorkloadsCommand(dependencies, &configPath))
	addGroupedCommand(command, "inspect", newEventsCommand(dependencies, &configPath))
	addGroupedCommand(command, "manage", newProfileCommand(dependencies, &configPath))
	addGroupedCommand(command, "manage", newDoctorCommand(dependencies))
	addGroupedCommand(command, "manage", newVersionCommand(dependencies.Version))

	return command
}

func addGroupedCommand(parent *cobra.Command, group string, child *cobra.Command) {
	child.GroupID = group
	parent.AddCommand(child)
}
