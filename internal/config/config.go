// Package config stores the NAS profiles syno talks to.
// Secrets live in the OS keyring, never in this file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/goccy/go-yaml"
)

// Config is the content of config.yaml.
type Config struct {
	// Current is the profile used when none is given explicitly.
	Current  string              `yaml:"current,omitempty"`
	Profiles map[string]*Profile `yaml:"profiles,omitempty"`
}

// Profile is one NAS and the account used on it.
type Profile struct {
	// URL is the DSM base URL, e.g. "https://192.168.1.10:5001".
	URL  string `yaml:"url"`
	User string `yaml:"user"`
	TLS  TLS    `yaml:"tls,omitempty"`
}

// TLS holds how the server certificate is trusted.
type TLS struct {
	// Pin is the SHA-256 of the server's public key, as "sha256/<base64>".
	// Empty means the certificate is verified against the system roots.
	Pin string `yaml:"pin,omitempty"`
}

// ErrNoProfile means no profile could be selected.
var ErrNoProfile = errors.New("no profile configured, run `syno login` first")

func dir() (string, error) {
	d := os.Getenv("XDG_CONFIG_HOME")
	if d == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		d = filepath.Join(home, ".config")
	}
	return filepath.Join(d, "syno"), nil
}

// Path returns the location of config.yaml.
func Path() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.yaml"), nil
}

// HasLegacy reports whether only the config.json of earlier versions exists.
func HasLegacy() bool {
	d, err := dir()
	if err != nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(d, "config.yaml")); err == nil {
		return false
	}
	_, err = os.Stat(filepath.Join(d, "config.json"))
	return err == nil
}

// Load returns an empty Config when the file does not exist yet.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return &c, nil
}

// Save writes the config with permissions only the user can read.
func (c *Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// Select picks a profile: name when given, then Current, then the only
// profile if there is exactly one.
func (c *Config) Select(name string) (string, *Profile, error) {
	if name == "" {
		name = c.Current
	}
	if name == "" && len(c.Profiles) == 1 {
		for n := range c.Profiles {
			name = n
		}
	}
	if name == "" {
		if len(c.Profiles) == 0 {
			return "", nil, ErrNoProfile
		}
		return "", nil, fmt.Errorf("several profiles exist (%v), choose one with --profile or `syno profile use`", c.Names())
	}
	p, ok := c.Profiles[name]
	if !ok {
		return "", nil, fmt.Errorf("no profile named %q, see `syno profile list`", name)
	}
	return name, p, nil
}

// Set adds or replaces a profile. The first profile becomes Current.
func (c *Config) Set(name string, p *Profile) {
	if c.Profiles == nil {
		c.Profiles = map[string]*Profile{}
	}
	c.Profiles[name] = p
	if c.Current == "" {
		c.Current = name
	}
}

// Remove deletes a profile and clears Current if it pointed to it.
func (c *Config) Remove(name string) {
	delete(c.Profiles, name)
	if c.Current == name {
		c.Current = ""
	}
}

// FindByURL returns the name of the profile for url and user, if any.
func (c *Config) FindByURL(url, user string) (string, bool) {
	for _, n := range c.Names() {
		p := c.Profiles[n]
		if p.URL == url && (user == "" || p.User == user) {
			return n, true
		}
	}
	return "", false
}

// SharesAccount reports whether another profile than name uses the same
// URL and user, and so the same keyring items.
func (c *Config) SharesAccount(name string) bool {
	p, ok := c.Profiles[name]
	if !ok {
		return false
	}
	for n, other := range c.Profiles {
		if n != name && other.URL == p.URL && other.User == p.User {
			return true
		}
	}
	return false
}

// Names returns the profile names in sorted order.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
