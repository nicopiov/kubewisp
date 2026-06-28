package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/doctor"
	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/kubectl"
)

type Service interface {
	Run(ctx context.Context, input io.Reader, output io.Writer, configPath string) error
}

type ProfileConnector interface {
	Connect(ctx context.Context, profile config.Profile) error
}

type Runner struct {
	connectivity kube.ConnectivityChecker
	namespaces   kube.NamespaceService
	pods         kube.PodService
	workloads    kube.WorkloadService
	network      kube.NetworkService
	events       kube.EventService
	yaml         kube.ResourceYAMLService
	doctor       doctor.Reporter
	portForward  kubectl.PortForwarder
	exec         kubectl.Executor
	profiles     ProfileConnector
}

func NewRunner(
	connectivity kube.ConnectivityChecker,
	namespaces kube.NamespaceService,
	pods kube.PodService,
	workloads kube.WorkloadService,
	network kube.NetworkService,
	events kube.EventService,
	yaml kube.ResourceYAMLService,
	doctorReporter doctor.Reporter,
	portForwarder kubectl.PortForwarder,
	executor kubectl.Executor,
	profileConnector ProfileConnector,
) *Runner {
	return &Runner{
		connectivity: connectivity,
		namespaces:   namespaces,
		pods:         pods,
		workloads:    workloads,
		network:      network,
		events:       events,
		yaml:         yaml,
		doctor:       doctorReporter,
		portForward:  portForwarder,
		exec:         executor,
		profiles:     profileConnector,
	}
}

func (r *Runner) Run(ctx context.Context, input io.Reader, output io.Writer, configPath string) error {
	store := config.Store{Path: configPath}
	cfg, err := store.Load()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no Kubewisp config found at %q; run `kubewisp init`", configPath)
		}
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
		Workloads:    r.workloads,
		Network:      r.network,
		Events:       r.events,
		YAML:         r.yaml,
		Doctor:       r.doctor,
		PortForward:  r.portForward,
		Exec:         r.exec,
		Profiles:     r.profiles,
		Clipboard:    NewOSC52Clipboard(output),
	})

	_, err = tea.NewProgram(
		model,
		tea.WithContext(ctx),
		tea.WithInput(input),
		tea.WithOutput(output),
		tea.WithAltScreen(),
	).Run()
	if err != nil {
		return fmt.Errorf("run dashboard: %w", err)
	}
	return nil
}
