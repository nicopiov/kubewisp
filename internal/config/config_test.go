package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validConfig() Config {
	cfg := New()
	cfg.CurrentProfile = "staging"
	cfg.Profiles["staging"] = Profile{
		Provider:         ProviderGKE,
		ProjectID:        "company-staging",
		ClusterName:      "staging-main",
		LocationType:     LocationRegion,
		Location:         "europe-west1",
		DefaultNamespace: "default",
	}
	return cfg
}

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "config.yaml")
	store := Store{Path: path}
	want := validConfig()

	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.CurrentProfile != want.CurrentProfile {
		t.Fatalf("CurrentProfile = %q, want %q", got.CurrentProfile, want.CurrentProfile)
	}
	if got.Profiles["staging"].ProjectID != "company-staging" {
		t.Fatalf("ProjectID = %q, want company-staging", got.Profiles["staging"].ProjectID)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("config permissions = %o, want 600", gotMode)
	}
}

func TestStoreLoadAppliesDefaultNamespace(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	data := `version: 1
profiles:
  staging:
    provider: gke
    projectId: company-staging
    clusterName: staging-main
    locationType: region
    location: europe-west1
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := (Store{Path: path}).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.Profiles["staging"].DefaultNamespace; got != "default" {
		t.Fatalf("DefaultNamespace = %q, want default", got)
	}
}

func TestValidateReportsInvalidProfile(t *testing.T) {
	t.Parallel()

	cfg := New()
	cfg.Profiles["broken"] = Profile{
		Provider:     ProviderGKE,
		LocationType: "planet",
	}

	err := Validate(cfg)

	for _, expected := range []string{
		`profile "broken"`,
		"projectId is required",
		"clusterName is required",
		`locationType must be "region" or "zone"`,
		"location is required",
	} {
		if err == nil || !strings.Contains(err.Error(), expected) {
			t.Fatalf("Validate() error = %v, want it to contain %q", err, expected)
		}
	}
}

func TestStoreLoadRejectsUnknownField(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nsurprise: true\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := (Store{Path: path}).Load()

	if err == nil || !strings.Contains(err.Error(), "field surprise not found") {
		t.Fatalf("Load() error = %v, want unknown field error", err)
	}
}
