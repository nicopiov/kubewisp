package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/nicopiov/kubewisp/internal/runner"
)

type fakeRunner struct {
	paths map[string]string
}

func (f fakeRunner) LookPath(binary string) (string, error) {
	path, ok := f.paths[binary]
	if !ok {
		return "", errors.New("not found")
	}
	return path, nil
}

func (fakeRunner) Run(context.Context, string, ...string) runner.CommandResult {
	panic("not used")
}

func (fakeRunner) RunInteractive(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
	panic("not used")
}

func TestDoctorCommandHealthy(t *testing.T) {
	t.Parallel()

	command := NewRootCommand(Dependencies{
		Runner: fakeRunner{
			paths: map[string]string{
				"gcloud":                 "/bin/gcloud",
				"kubectl":                "/bin/kubectl",
				"gke-gcloud-auth-plugin": "/bin/gke-gcloud-auth-plugin",
			},
		},
	})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"doctor"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "All required dependencies are available.") {
		t.Fatalf("unexpected output:\n%s", output.String())
	}
}

func TestDoctorCommandMissingDependency(t *testing.T) {
	t.Parallel()

	command := NewRootCommand(Dependencies{
		Runner: fakeRunner{
			paths: map[string]string{
				"kubectl": "/bin/kubectl",
			},
		},
	})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"doctor"})

	err := command.Execute()

	if !errors.Is(err, ErrMissingDependencies) {
		t.Fatalf("Execute() error = %v, want ErrMissingDependencies", err)
	}
	if !strings.Contains(output.String(), "FAIL  gcloud") {
		t.Fatalf("unexpected output:\n%s", output.String())
	}
}

func TestRootHelpGroupsCommandsAndShowsExamples(t *testing.T) {
	t.Parallel()

	command := NewRootCommand(Dependencies{Runner: fakeRunner{}})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--help"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, expected := range []string{
		"Start and setup:", "Inspect and operate:", "Manage Kubewisp:",
		"kubewisp profile add", "kubewisp workloads restart Deployment/api",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("help missing %q:\n%s", expected, output.String())
		}
	}
}
