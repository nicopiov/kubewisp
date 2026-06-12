package tui

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/nicopiov/kubewisp/internal/kubectl"
)

type recordingPortForwarder struct {
	options kubectl.PortForwardOptions
}

func (f *recordingPortForwarder) PortForward(
	_ context.Context,
	_ io.Reader,
	_, _ io.Writer,
	options kubectl.PortForwardOptions,
) error {
	f.options = options
	return nil
}

func TestPortForwardCommandRunsForwarder(t *testing.T) {
	t.Parallel()

	forwarder := &recordingPortForwarder{}
	options := kubectl.PortForwardOptions{
		Namespace:  "api",
		Pod:        "api-abc",
		LocalPort:  8080,
		RemotePort: 8080,
	}
	var output bytes.Buffer
	command := &portForwardCommand{forwarder: forwarder, options: options}
	command.SetStdout(&output)
	command.SetStderr(&bytes.Buffer{})

	if err := command.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(forwarder.options, options) {
		t.Fatalf("options = %#v, want %#v", forwarder.options, options)
	}
	if got := output.String(); got == "" {
		t.Fatal("Run() did not print stop guidance")
	}
}
