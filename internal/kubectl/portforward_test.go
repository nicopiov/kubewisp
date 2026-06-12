package kubectl

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/nicopiov/kubewisp/internal/runner"
)

type fakeRunner struct {
	name string
	args []string
}

func (f *fakeRunner) LookPath(string) (string, error) {
	return "", nil
}

func (f *fakeRunner) Run(context.Context, string, ...string) runner.CommandResult {
	return runner.CommandResult{}
}

func (f *fakeRunner) RunInteractive(
	_ context.Context,
	_ io.Reader,
	_, _ io.Writer,
	name string,
	args ...string,
) error {
	f.name = name
	f.args = args
	return nil
}

func TestPortForwardBuildsKubectlCommand(t *testing.T) {
	t.Parallel()

	commandRunner := &fakeRunner{}
	service := NewService(commandRunner)
	err := service.PortForward(
		context.Background(),
		nil,
		&bytes.Buffer{},
		&bytes.Buffer{},
		PortForwardOptions{Namespace: "api", Pod: "api-abc", LocalPort: 18080, RemotePort: 8080},
	)
	if err != nil {
		t.Fatalf("PortForward() error = %v", err)
	}

	want := []string{"--namespace", "api", "port-forward", "pod/api-abc", "18080:8080"}
	if commandRunner.name != "kubectl" || !reflect.DeepEqual(commandRunner.args, want) {
		t.Fatalf("command = %s %#v, want kubectl %#v", commandRunner.name, commandRunner.args, want)
	}
}
