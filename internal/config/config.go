package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AllowedRoots    []string      `yaml:"allowed_roots"`
	AllowedCommands []string      `yaml:"allowed_commands"`
	Timeout         time.Duration `yaml:"timeout"`
	MaxOutputBytes  int           `yaml:"max_output_bytes"`
	CodexExtraArgs  []string      `yaml:"codex_extra_args"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if c.Timeout == 0 {
		c.Timeout = 180 * time.Second
	}
	if c.MaxOutputBytes == 0 {
		c.MaxOutputBytes = 200000
	}
	if len(c.AllowedRoots) == 0 {
		return nil, fmt.Errorf("allowed_roots must not be empty")
	}
	for i, r := range c.AllowedRoots {
		abs, err := filepath.Abs(r)
		if err != nil {
			return nil, fmt.Errorf("allowed_root %q: %w", r, err)
		}
		c.AllowedRoots[i] = abs
	}
	return &c, nil
}

func (c *Config) CommandAllowed(name string) bool {
	for _, a := range c.AllowedCommands {
		if a == name {
			return true
		}
	}
	return false
}

func (c *Config) ResolveAllowedDir(dir string) (string, error) {
	if strings.HasPrefix(dir, "/sessions/") {
		return "", fmt.Errorf("got a sandbox path %q; pass the host path (e.g. /Users/...) instead", dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve dir: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve dir: %w", err)
	}
	for _, root := range c.AllowedRoots {
		rootResolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if resolved == rootResolved || strings.HasPrefix(resolved, rootResolved+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("path %q is not inside any allowed_root", dir)
}
