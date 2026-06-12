package cli

import (
	"bytes"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	t.Parallel()

	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, Version: "v0.2.0"})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"version"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := output.String(); got != "kubewisp v0.2.0\n" {
		t.Fatalf("output = %q", got)
	}
}
