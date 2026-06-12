package tui

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerMissingConfigSuggestsInit(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing", "config.yaml")
	err := (&Runner{}).Run(context.Background(), nil, &bytes.Buffer{}, path)

	if err == nil || !strings.Contains(err.Error(), "run `kubewisp init`") {
		t.Fatalf("Run() error = %v, want init guidance", err)
	}
	if strings.Contains(err.Error(), "load dashboard config") {
		t.Fatalf("Run() error exposes dashboard config implementation: %v", err)
	}
}
