package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
)

type OSRunner struct{}

func NewOSRunner() *OSRunner {
	return &OSRunner{}
}

func (r *OSRunner) LookPath(binary string) (string, error) {
	return exec.LookPath(binary)
}

func (r *OSRunner) RunInteractive(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	name string,
	args ...string,
) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func (r *OSRunner) Run(ctx context.Context, name string, args ...string) CommandResult {
	command := exec.CommandContext(ctx, name, args...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	result := CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
		Err:      err,
	}

	if err == nil {
		return result
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		result.ExitCode = exitError.ExitCode()
	} else {
		result.ExitCode = -1
	}

	return result
}
