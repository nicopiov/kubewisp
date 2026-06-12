package runner

import (
	"context"
	"io"
)

type CommandRunner interface {
	LookPath(binary string) (string, error)
	Run(ctx context.Context, name string, args ...string) CommandResult
	RunInteractive(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error
}

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}
