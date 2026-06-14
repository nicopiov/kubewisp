package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/runner"
)

type fakeTUI struct {
	path  string
	calls int
}

func (f *fakeTUI) Run(context.Context, io.Reader, io.Writer, string) error {
	f.calls++
	return nil
}

func TestRootCommandOpensTUI(t *testing.T) {
	t.Parallel()

	dashboard := &fakeTUI{}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, TUI: dashboard})
	command.SetOut(&bytes.Buffer{})
	command.SetArgs(nil)

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if dashboard.calls != 1 {
		t.Fatalf("TUI calls = %d, want 1", dashboard.calls)
	}
}

func TestTUICommandOpensTUI(t *testing.T) {
	t.Parallel()

	dashboard := &fakeTUI{}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, TUI: dashboard})
	command.SetOut(&bytes.Buffer{})
	command.SetArgs([]string{"tui"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if dashboard.calls != 1 {
		t.Fatalf("TUI calls = %d, want 1", dashboard.calls)
	}
}

type startupConnectivity struct {
	results []error
}

func (s *startupConnectivity) Check(context.Context, string) (kube.ConnectivityReport, error) {
	err := s.results[0]
	s.results = s.results[1:]
	return kube.ConnectivityReport{ServerVersion: "v1.32.1"}, err
}

func TestTUIStartupSelectsAndConnectsProfile(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	dashboard := &fakeTUI{}
	commandRunner := &initRunner{results: []runner.CommandResult{{}, {}}}
	profileSelector := &fakeSelector{selected: "production"}
	command := NewRootCommand(Dependencies{
		Runner:       commandRunner,
		Connectivity: &startupConnectivity{results: []error{nil}},
		Selector:     profileSelector,
		TUI:          dashboard,
	})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	cfg, err := (config.Store{Path: path}).Load()
	if err != nil || cfg.CurrentProfile != "production" {
		t.Fatalf("selected config = %#v, error = %v", cfg, err)
	}
	if profileSelector.initial != "staging" || dashboard.calls != 1 {
		t.Fatalf("selector = %#v, TUI calls = %d", profileSelector, dashboard.calls)
	}
	if got := commandRunner.calls[1].args; !strings.Contains(strings.Join(got, " "), "production-main") ||
		!strings.Contains(strings.Join(got, " "), "company-production") {
		t.Fatalf("get-credentials args = %#v, want selected production profile", got)
	}
	for _, expected := range []string{
		"Preparing Kubewisp", "Loading saved profiles", "Activating Google Cloud project",
		"Refreshing GKE credentials", "Verifying Kubernetes API access", "Opening dashboard",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("startup output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestTUIStartupReauthenticatesExpiredGcloudSession(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	dashboard := &fakeTUI{}
	commandRunner := &initRunner{results: []runner.CommandResult{
		{}, {},
		{Stdout: "developer@company.com\n"},
		{}, {},
	}}
	command := NewRootCommand(Dependencies{
		Runner: commandRunner,
		Connectivity: &startupConnectivity{results: []error{
			errors.New("reach Kubernetes API: Google Cloud authentication expired; run `gcloud auth login`"),
			nil,
		}},
		Selector: &fakeSelector{selected: "staging"},
		TUI:      dashboard,
	})
	var output bytes.Buffer
	command.SetIn(strings.NewReader("\n"))
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--config", path})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output.String())
	}
	if len(commandRunner.interactiveCalls) != 1 || dashboard.calls != 1 {
		t.Fatalf("interactive calls = %d, TUI calls = %d", len(commandRunner.interactiveCalls), dashboard.calls)
	}
	for _, expected := range []string{
		"authentication has expired or was revoked", "Run gcloud auth login now?",
		"no terminal input is required", "authentication completed", "Reauthenticated and connected",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output missing %q:\n%s", expected, output.String())
		}
	}
}
