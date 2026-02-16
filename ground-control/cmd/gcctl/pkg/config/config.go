package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigDir  = ".gcctl"
	DefaultConfigFile = "config.yaml"
)

type Config struct {
	Server       string `yaml:"server"`
	Token        string `yaml:"token,omitempty"`
	ExpiresAt    string `yaml:"expires_at,omitempty"`
	Username     string `yaml:"username,omitempty"`
	OutputFormat string `yaml:"output_format,omitempty"`
}

func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine home directory: %w", err)
	}
	return filepath.Join(home, DefaultConfigDir, DefaultConfigFile), nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

func (c *Config) ClearCredentials() {
	c.Token = ""
	c.ExpiresAt = ""
	c.Username = ""
}

func (c *Config) HasCredentials() bool {
	return c.Token != ""
}