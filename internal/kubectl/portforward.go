package kubectl

import (
	"context"
	"fmt"
	"io"

	"github.com/nicopiov/kubewisp/internal/runner"
)

type PortForwardOptions struct {
	Namespace  string
	Pod        string
	LocalPort  int32
	RemotePort int32
}

type PortForwarder interface {
	PortForward(context.Context, io.Reader, io.Writer, io.Writer, PortForwardOptions) error
}

type Service struct {
	runner runner.CommandRunner
}

func NewService(commandRunner runner.CommandRunner) *Service {
	return &Service{runner: commandRunner}
}

func (s *Service) PortForward(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	options PortForwardOptions,
) error {
	target := fmt.Sprintf("%d:%d", options.LocalPort, options.RemotePort)
	err := s.runner.RunInteractive(
		ctx,
		stdin,
		stdout,
		stderr,
		"kubectl",
		"--namespace", options.Namespace,
		"port-forward",
		"pod/"+options.Pod,
		target,
	)
	if err != nil {
		return fmt.Errorf("port-forward pod %q in namespace %q: %w", options.Pod, options.Namespace, err)
	}
	return nil
}
