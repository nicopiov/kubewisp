package tui

import (
	"context"
	"fmt"
	"io"

	"github.com/nicopiov/kubewisp/internal/kubectl"
)

type portForwardCommand struct {
	forwarder kubectl.PortForwarder
	options   kubectl.PortForwardOptions
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
}

func (c *portForwardCommand) SetStdin(stdin io.Reader) {
	c.stdin = stdin
}

func (c *portForwardCommand) SetStdout(stdout io.Writer) {
	c.stdout = stdout
}

func (c *portForwardCommand) SetStderr(stderr io.Writer) {
	c.stderr = stderr
}

func (c *portForwardCommand) Run() error {
	fmt.Fprintf(
		c.stdout,
		"Forwarding localhost:%d to %s/%s:%d. Press Ctrl+C to stop and return to Kubewisp.\n",
		c.options.LocalPort,
		c.options.Namespace,
		c.options.Pod,
		c.options.RemotePort,
	)
	return c.forwarder.PortForward(
		context.Background(),
		c.stdin,
		c.stdout,
		c.stderr,
		c.options,
	)
}
