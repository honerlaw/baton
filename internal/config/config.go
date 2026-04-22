// Package config loads tool-level settings with XDG precedence and
// env-var overrides.
//
// Precedence (high → low):
//
//	BATON_MODEL env var
//	$XDG_CONFIG_HOME/baton/config.yaml
//	~/.config/baton/config.yaml
//	compiled-in defaults
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds runtime-configurable settings.
type Config struct {
	DefaultModel  string `yaml:"default_model"`
	APIKey        string `yaml:"-"` // sourced only from env
	APIKeyEnvVar  string `yaml:"api_key_env_var"`
	ArtifactsRoot string `yaml:"artifacts_root"`
}

// Defaults returns the compiled-in defaults.
func Defaults() Config {
	return Config{
		DefaultModel:  "anthropic/claude-sonnet-4.6",
		APIKeyEnvVar:  "OPENROUTER_API_KEY",
		ArtifactsRoot: ".baton/runs",
	}
}

// Load merges XDG config and env overrides onto Defaults.
func Load() (Config, error) {
	c := Defaults()
	path, err := configFilePath()
	if err == nil {
		if b, err := os.ReadFile(path); err == nil {
			var fileCfg Config
			if err := yaml.Unmarshal(b, &fileCfg); err != nil {
				return c, fmt.Errorf("parse %s: %w", path, err)
			}
			if fileCfg.DefaultModel != "" {
				c.DefaultModel = fileCfg.DefaultModel
			}
			if fileCfg.APIKeyEnvVar != "" {
				c.APIKeyEnvVar = fileCfg.APIKeyEnvVar
			}
			if fileCfg.ArtifactsRoot != "" {
				c.ArtifactsRoot = fileCfg.ArtifactsRoot
			}
		}
	}
	if m := os.Getenv("BATON_MODEL"); m != "" {
		c.DefaultModel = m
	}
	if r := os.Getenv("BATON_ARTIFACTS_ROOT"); r != "" {
		c.ArtifactsRoot = r
	}
	c.APIKey = os.Getenv(c.APIKeyEnvVar)
	return c, nil
}

// configFilePath returns the preferred config file path, preferring
// XDG_CONFIG_HOME and falling back to ~/.config.
func configFilePath() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "baton", "config.yaml"), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".config", "baton", "config.yaml"), nil
}
