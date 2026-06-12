package tui

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"testing"

	"github.com/nicopiov/kubewisp/internal/kubectl"
)

type recordingExecutor struct {
	options kubectl.ExecOptions
}

func (f *recordingExecutor) Exec(
	_ context.Context,
	_ io.Reader,
	_, _ io.Writer,
	options kubectl.ExecOptions,
) error {
	f.options = options
	return nil
}

func TestExecCommandRunsExecutor(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	options := kubectl.ExecOptions{
		Namespace: "api",
		Pod:       "api-abc",
		Container: "app",
		Command:   "/bin/sh",
	}
	var output bytes.Buffer
	command := &execCommand{executor: executor, options: options}
	command.SetStdout(&output)
	command.SetStderr(&bytes.Buffer{})

	if err := command.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(executor.options, options) {
		t.Fatalf("options = %#v, want %#v", executor.options, options)
	}
	if got := output.String(); got == "" {
		t.Fatal("Run() did not print return guidance")
	}
}
