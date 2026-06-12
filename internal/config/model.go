package config

const (
	CurrentVersion = 1
	ProviderGKE    = "gke"
	LocationRegion = "region"
	LocationZone   = "zone"
)

type Config struct {
	Version        int                `yaml:"version"`
	CurrentProfile string             `yaml:"currentProfile,omitempty"`
	Settings       Settings           `yaml:"settings"`
	Profiles       map[string]Profile `yaml:"profiles"`
}

type Settings struct {
	ConfirmDestructiveActions bool `yaml:"confirmDestructiveActions"`
	OpenDashboardAfterInit    bool `yaml:"openDashboardAfterInit"`
}

type Profile struct {
	Provider                      string `yaml:"provider"`
	ExpectedGoogleWorkspaceDomain string `yaml:"expectedGoogleWorkspaceDomain,omitempty"`
	ProjectID                     string `yaml:"projectId"`
	ClusterName                   string `yaml:"clusterName"`
	LocationType                  string `yaml:"locationType"`
	Location                      string `yaml:"location"`
	DefaultNamespace              string `yaml:"defaultNamespace"`
	CurrentNamespace              string `yaml:"currentNamespace,omitempty"`
	ContextAlias                  string `yaml:"contextAlias,omitempty"`
	Production                    bool   `yaml:"production"`
}

func New() Config {
	return Config{
		Version: CurrentVersion,
		Settings: Settings{
			ConfirmDestructiveActions: true,
			OpenDashboardAfterInit:    true,
		},
		Profiles: make(map[string]Profile),
	}
}

func (c *Config) ApplyDefaults() {
	if c.Profiles == nil {
		c.Profiles = make(map[string]Profile)
	}

	for name, profile := range c.Profiles {
		if profile.DefaultNamespace == "" {
			profile.DefaultNamespace = "default"
		}
		c.Profiles[name] = profile
	}
}
