package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nicopiov/kubewisp/internal/config"
	"github.com/nicopiov/kubewisp/internal/kube"
	"github.com/nicopiov/kubewisp/internal/runner"
)

type initCall struct {
	name string
	args []string
}

type initRunner struct {
	results          []runner.CommandResult
	calls            []initCall
	interactiveCalls []initCall
}

type initConnectivity struct {
	report kube.ConnectivityReport
	err    error
}

func (c initConnectivity) Check(context.Context, string) (kube.ConnectivityReport, error) {
	return c.report, c.err
}

func (r *initRunner) LookPath(binary string) (string, error) {
	switch binary {
	case "gcloud", "kubectl", "gke-gcloud-auth-plugin":
		return "/bin/" + binary, nil
	default:
		return "", errors.New("not found")
	}
}

func (r *initRunner) Run(_ context.Context, name string, args ...string) runner.CommandResult {
	r.calls = append(r.calls, initCall{name: name, args: args})
	result := r.results[0]
	r.results = r.results[1:]
	return result
}

func (r *initRunner) RunInteractive(_ context.Context, _ io.Reader, _, _ io.Writer, name string, args ...string) error {
	r.interactiveCalls = append(r.interactiveCalls, initCall{name: name, args: args})
	return nil
}

func TestInitDiscoversClusterAndSavesProfile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	commandRunner := &initRunner{
		results: []runner.CommandResult{
			{Stdout: "developer@company.com\n"},
			{Stdout: "company-staging\n"},
			{Stdout: `[{"name":"staging-main","location":"europe-west1"}]`},
			{},
			{},
		},
	}
	command := NewRootCommand(Dependencies{
		Runner: commandRunner,
		Connectivity: initConnectivity{
			report: kube.ConnectivityReport{ServerVersion: "v1.32.1", Namespace: "default"},
		},
	})
	var output bytes.Buffer
	command.SetIn(strings.NewReader("\n\n\n"))
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--config", path, "init"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output.String())
	}

	cfg, err := (config.Store{Path: path}).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	profile := cfg.Profiles["staging-main"]
	if cfg.CurrentProfile != "staging-main" {
		t.Fatalf("CurrentProfile = %q, want staging-main", cfg.CurrentProfile)
	}
	if profile.ProjectID != "company-staging" || profile.LocationType != config.LocationRegion {
		t.Fatalf("profile = %#v", profile)
	}

	wantCredentialsArgs := []string{
		"container", "clusters", "get-credentials", "staging-main",
		"--region", "europe-west1", "--project", "company-staging",
	}
	if got := commandRunner.calls[4].args; !reflect.DeepEqual(got, wantCredentialsArgs) {
		t.Fatalf("get-credentials args = %#v, want %#v", got, wantCredentialsArgs)
	}
}

func TestInitLogsInWhenNoAccountIsActive(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	commandRunner := &initRunner{
		results: []runner.CommandResult{
			{},
			{Stdout: "developer@company.com\n"},
			{Stdout: "company-staging\n"},
			{Stdout: `[{"name":"staging-main","location":"europe-west1-b"}]`},
			{},
			{},
		},
	}
	command := NewRootCommand(Dependencies{
		Runner: commandRunner,
		Connectivity: initConnectivity{
			report: kube.ConnectivityReport{ServerVersion: "v1.32.1", Namespace: "default"},
		},
	})
	var output bytes.Buffer
	command.SetIn(strings.NewReader("\n\n\n\n"))
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--config", path, "init"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output.String())
	}
	if got := len(commandRunner.interactiveCalls); got != 1 {
		t.Fatalf("interactive calls = %d, want 1", got)
	}
	if got := commandRunner.interactiveCalls[0].args; !reflect.DeepEqual(got, []string{"auth", "login"}) {
		t.Fatalf("interactive args = %#v, want auth login", got)
	}
}

func TestInitReauthenticatesWhenProjectDiscoveryTokenExpired(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	commandRunner := &initRunner{
		results: []runner.CommandResult{
			{Stdout: "developer@company.com\n"},
			{Err: errors.New("exit 1"), Stderr: "There was a problem refreshing your current auth tokens: reauthentication failed"},
			{Stdout: "developer@company.com\n"},
			{Stdout: "company-staging\n"},
			{Stdout: `[{"name":"staging-main","location":"europe-west1"}]`},
			{},
			{},
		},
	}
	command := NewRootCommand(Dependencies{
		Runner: commandRunner,
		Connectivity: initConnectivity{
			report: kube.ConnectivityReport{ServerVersion: "v1.32.1", Namespace: "default"},
		},
	})
	var output bytes.Buffer
	command.SetIn(strings.NewReader("\n\n\n\n"))
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"--config", path, "init"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\n%s", err, output.String())
	}
	if got := len(commandRunner.interactiveCalls); got != 1 {
		t.Fatalf("interactive calls = %d, want 1", got)
	}
	for _, expected := range []string{"authentication has expired or was revoked", "Run gcloud auth login now?"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output.String())
		}
	}
}
