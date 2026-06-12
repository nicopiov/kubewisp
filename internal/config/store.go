package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const EnvPath = "KUBEWISP_CONFIG"

type Store struct {
	Path string
}

func DefaultPath() (string, error) {
	if path := os.Getenv(EnvPath); path != "" {
		return path, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".config", "kubewisp", "config.yaml"), nil
}

func (s Store) Load() (Config, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", s.Path, err)
	}

	cfg := New()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", s.Path, err)
	}
	cfg.ApplyDefaults()

	if err := Validate(cfg); err != nil {
		return Config{}, fmt.Errorf("validate config %q: %w", s.Path, err)
	}
	return cfg, nil
}

func (s Store) Save(cfg Config) error {
	cfg.ApplyDefaults()
	if err := Validate(cfg); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory %q: %w", dir, err)
	}

	temp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("set temporary config permissions: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(tempPath, s.Path); err != nil {
		return fmt.Errorf("replace config %q: %w", s.Path, err)
	}

	return nil
}
