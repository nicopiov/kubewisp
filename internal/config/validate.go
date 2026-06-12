package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

func Validate(cfg Config) error {
	var validationErrors []error

	if cfg.Version != CurrentVersion {
		validationErrors = append(validationErrors, fmt.Errorf(
			"unsupported config version %d; expected %d",
			cfg.Version,
			CurrentVersion,
		))
	}

	if cfg.CurrentProfile != "" {
		if _, ok := cfg.Profiles[cfg.CurrentProfile]; !ok {
			validationErrors = append(validationErrors, fmt.Errorf(
				"current profile %q does not exist",
				cfg.CurrentProfile,
			))
		}
	}

	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			validationErrors = append(validationErrors, errors.New("profile name cannot be empty"))
			continue
		}
		if err := validateProfile(cfg.Profiles[name]); err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("profile %q: %w", name, err))
		}
	}

	return errors.Join(validationErrors...)
}

func validateProfile(profile Profile) error {
	var validationErrors []error

	if profile.Provider == "" {
		validationErrors = append(validationErrors, errors.New("provider is required"))
	} else if profile.Provider != ProviderGKE {
		validationErrors = append(validationErrors, fmt.Errorf(
			"provider %q is not supported; expected %q",
			profile.Provider,
			ProviderGKE,
		))
	}

	required := []struct {
		name  string
		value string
	}{
		{name: "projectId", value: profile.ProjectID},
		{name: "clusterName", value: profile.ClusterName},
		{name: "locationType", value: profile.LocationType},
		{name: "location", value: profile.Location},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			validationErrors = append(validationErrors, fmt.Errorf("%s is required", field.name))
		}
	}

	if profile.LocationType != "" &&
		profile.LocationType != LocationRegion &&
		profile.LocationType != LocationZone {
		validationErrors = append(validationErrors, fmt.Errorf(
			"locationType must be %q or %q",
			LocationRegion,
			LocationZone,
		))
	}

	return errors.Join(validationErrors...)
}
