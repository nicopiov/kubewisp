package runner

import (
	"bytes"
	"context"
	"testing"
)

func TestOSRunnerRun(t *testing.T) {
	t.Parallel()

	result := NewOSRunner().Run(context.Background(), "go", "version")

	if result.Err != nil {
		t.Fatalf("Run() error = %v", result.Err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("Run() exit code = %d, want 0", result.ExitCode)
	}
	if result.Stdout == "" {
		t.Fatal("Run() stdout is empty")
	}
}

func TestOSRunnerRunInteractive(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	err := NewOSRunner().RunInteractive(
		context.Background(),
		nil,
		&stdout,
		&bytes.Buffer{},
		"go",
		"version",
	)

	if err != nil {
		t.Fatalf("RunInteractive() error = %v", err)
	}
	if stdout.Len() == 0 {
		t.Fatal("RunInteractive() stdout is empty")
	}
}

func TestOSRunnerMissingCommand(t *testing.T) {
	t.Parallel()

	result := NewOSRunner().Run(context.Background(), "kubewisp-command-that-does-not-exist")

	if result.Err == nil {
		t.Fatal("Run() error = nil, want an error")
	}
	if result.ExitCode != -1 {
		t.Fatalf("Run() exit code = %d, want -1", result.ExitCode)
	}
}
