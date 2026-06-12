package cli

import (
	"bytes"
	"context"
	"io"
	"testing"
)

type fakeTUI struct {
	path  string
	calls int
}

func (f *fakeTUI) Run(context.Context, io.Reader, io.Writer, string) error {
	f.calls++
	return nil
}

func TestRootCommandOpensTUI(t *testing.T) {
	t.Parallel()

	dashboard := &fakeTUI{}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, TUI: dashboard})
	command.SetOut(&bytes.Buffer{})
	command.SetArgs(nil)

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if dashboard.calls != 1 {
		t.Fatalf("TUI calls = %d, want 1", dashboard.calls)
	}
}

func TestTUICommandOpensTUI(t *testing.T) {
	t.Parallel()

	dashboard := &fakeTUI{}
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}, TUI: dashboard})
	command.SetOut(&bytes.Buffer{})
	command.SetArgs([]string{"tui"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if dashboard.calls != 1 {
		t.Fatalf("TUI calls = %d, want 1", dashboard.calls)
	}
}
