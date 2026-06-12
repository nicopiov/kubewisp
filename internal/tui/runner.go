package tui

import (
	"context"
	"errors"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/doctor"
	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/kubectl"
)

type Service interface {
	Run(ctx context.Context, input io.Reader, output io.Writer, configPath string) error
}

type Runner struct {
	connectivity kube.ConnectivityChecker
	namespaces   kube.NamespaceService
	pods         kube.PodService
	doctor       doctor.Reporter
	portForward  kubectl.PortForwarder
}

func NewRunner(
	connectivity kube.ConnectivityChecker,
	namespaces kube.NamespaceService,
	pods kube.PodService,
	doctorReporter doctor.Reporter,
	portForwarder kubectl.PortForwarder,
) *Runner {
	return &Runner{
		connectivity: connectivity,
		namespaces:   namespaces,
		pods:         pods,
		doctor:       doctorReporter,
		portForward:  portForwarder,
	}
}

func (r *Runner) Run(ctx context.Context, input io.Reader, output io.Writer, configPath string) error {
	store := config.Store{Path: configPath}
	cfg, err := store.Load()
	if err != nil {
		return fmt.Errorf("load dashboard config: %w", err)
	}
	if cfg.CurrentProfile == "" {
		return errors.New("no current profile; run `kubewisp init`")
	}

	profile, ok := cfg.Profiles[cfg.CurrentProfile]
	if !ok {
		return fmt.Errorf("current profile %q does not exist", cfg.CurrentProfile)
	}
	model := NewModel(Dependencies{
		ConfigPath:   configPath,
		ProfileName:  cfg.CurrentProfile,
		Profile:      profile,
		Connectivity: r.connectivity,
		Namespaces:   r.namespaces,
		Pods:         r.pods,
		Doctor:       r.doctor,
	})

	result, err := tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithInput(input),
		tea.WithOutput(output),
		tea.WithAltScreen(),
	).Run()
	if err != nil {
		return fmt.Errorf("run dashboard: %w", err)
	}
	finalModel, ok := result.(Model)
	if !ok || finalModel.portForward == nil {
		return nil
	}
	if r.portForward == nil {
		return errors.New("kubectl port-forward service is not configured")
	}
	options := *finalModel.portForward
	fmt.Fprintf(
		output,
		"Forwarding localhost:%d to %s/%s:%d. Press Ctrl+C to stop.\n",
		options.LocalPort,
		options.Namespace,
		options.Pod,
		options.RemotePort,
	)
	if err := r.portForward.PortForward(ctx, input, output, output, options); err != nil {
		return err
	}
	return nil
}
