package tui

import (
	"context"
	"fmt"
	"io"

	"github.com/nicopiov/kubewisp/internal/kubectl"
)

type execCommand struct {
	executor kubectl.Executor
	options  kubectl.ExecOptions
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
}

func (c *execCommand) SetStdin(stdin io.Reader) {
	c.stdin = stdin
}

func (c *execCommand) SetStdout(stdout io.Writer) {
	c.stdout = stdout
}

func (c *execCommand) SetStderr(stderr io.Writer) {
	c.stderr = stderr
}

func (c *execCommand) Run() error {
	fmt.Fprintf(
		c.stdout,
		"Opening %s in %s/%s container %s. Exit the shell to return to Kubewisp.\n",
		c.options.Command,
		c.options.Namespace,
		c.options.Pod,
		c.options.Container,
	)
	return c.executor.Exec(
		context.Background(),
		c.stdin,
		c.stdout,
		c.stderr,
		c.options,
	)
}
