package kubectl

import (
	"context"
	"fmt"
	"io"
)

type ExecOptions struct {
	Namespace string
	Pod       string
	Container string
	Command   string
}

type Executor interface {
	Exec(context.Context, io.Reader, io.Writer, io.Writer, ExecOptions) error
}

func (s *Service) Exec(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	options ExecOptions,
) error {
	err := s.runner.RunInteractive(
		ctx,
		stdin,
		stdout,
		stderr,
		"kubectl",
		"--namespace", options.Namespace,
		"exec",
		"-it",
		"pod/"+options.Pod,
		"--container", options.Container,
		"--",
		options.Command,
	)
	if err != nil {
		return fmt.Errorf(
			"exec %q in container %q of pod %q in namespace %q: %w",
			options.Command,
			options.Container,
			options.Pod,
			options.Namespace,
			err,
		)
	}
	return nil
}
