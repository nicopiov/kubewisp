package doctor

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

func TestServiceRunReportsAllDependencies(t *testing.T) {
	t.Parallel()

	service := NewService(fakeRunner{
		paths: map[string]string{
			"kubectl": "/usr/local/bin/kubectl",
		},
	})

	report := service.Run(context.Background())

	if report.Healthy() {
		t.Fatal("Report.Healthy() = true, want false")
	}
	if got, want := len(report.Checks), 3; got != want {
		t.Fatalf("len(Report.Checks) = %d, want %d", got, want)
	}

	var output bytes.Buffer
	WriteReport(&output, report)

	for _, expected := range []string{
		"FAIL  gcloud",
		"PASS  kubectl",
		"FAIL  gke-gcloud-auth-plugin",
		"One or more required dependencies are missing.",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output.String())
		}
	}
}
