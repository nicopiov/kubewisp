package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicopiov/kubewisp/internal/config"
)

func writeProfileTestConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.New()
	cfg.CurrentProfile = "staging"
	cfg.Profiles["staging"] = config.Profile{
		Provider:         config.ProviderGKE,
		ProjectID:        "company-staging",
		ClusterName:      "staging-main",
		LocationType:     config.LocationRegion,
		Location:         "europe-west1",
		DefaultNamespace: "api",
	}
	cfg.Profiles["production"] = config.Profile{
		Provider:         config.ProviderGKE,
		ProjectID:        "company-production",
		ClusterName:      "production-main",
		LocationType:     config.LocationRegion,
		Location:         "europe-west1",
		DefaultNamespace: "default",
		Production:       true,
	}
	if err := (config.Store{Path: path}).Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	return path
}

func TestProfileList(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "profile", "list"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	for _, expected := range []string{
		"production",
		"* staging",
		"company-staging / staging-main / api",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output.String())
		}
	}
}

func TestProfileUsePersistsSelection(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "profile", "use", "production"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	cfg, err := (config.Store{Path: path}).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.CurrentProfile != "production" {
		t.Fatalf("CurrentProfile = %q, want production", cfg.CurrentProfile)
	}
}

func TestProfileShowCurrent(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--config", path, "profile", "show"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(output.String(), "Name: staging") {
		t.Fatalf("unexpected output:\n%s", output.String())
	}
}

func TestProfileRenameUpdatesCurrentProfile(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}})
	command.SetArgs([]string{"--config", path, "profile", "rename", "staging", "development"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	cfg, err := (config.Store{Path: path}).Load()
	if err != nil || cfg.CurrentProfile != "development" || cfg.Profiles["development"].ClusterName != "staging-main" {
		t.Fatalf("renamed config = %#v, error = %v", cfg, err)
	}
}

func TestProfileDeleteRequiresConfirmationAndSelectsNext(t *testing.T) {
	t.Parallel()

	path := writeProfileTestConfig(t)
	command := NewRootCommand(Dependencies{Runner: fakeRunner{}})
	command.SetIn(strings.NewReader("y\n"))
	command.SetArgs([]string{"--config", path, "profile", "delete", "staging"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	cfg, err := (config.Store{Path: path}).Load()
	if err != nil || cfg.CurrentProfile != "production" {
		t.Fatalf("deleted config = %#v, error = %v", cfg, err)
	}
	if _, exists := cfg.Profiles["staging"]; exists {
		t.Fatal("deleted profile still exists")
	}
}
