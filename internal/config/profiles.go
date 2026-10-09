// Package config reads and writes syno's files in ~/.config/syno:
// profiles.yaml, which syno writes, and config.yaml, which users write.
// Secrets live in the OS keyring, never in these files.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/goccy/go-yaml"
)

// Profiles is the content of profiles.yaml. syno rewrites this file, so
// users should not keep comments in it.
type Profiles struct {
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

func path(name string) (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, name), nil
}

// ProfilesPath returns the location of profiles.yaml.
func ProfilesPath() (string, error) { return path("profiles.yaml") }

// LegacyHint explains what to do when only files of earlier versions exist:
// config.json, or a config.yaml that still holds the profiles. It returns ""
// when there is nothing to migrate.
func LegacyHint() string {
	d, err := dir()
	if err != nil {
		return ""
	}
	if exists(filepath.Join(d, "profiles.yaml")) {
		return ""
	}
	if b, err := os.ReadFile(filepath.Join(d, "config.yaml")); err == nil {
		var top map[string]any
		if yaml.Unmarshal(b, &top) == nil {
			if _, ok := top["profiles"]; ok {
				return fmt.Sprintf("profiles moved from config.yaml to profiles.yaml, rename %s to %s",
					filepath.Join(d, "config.yaml"), filepath.Join(d, "profiles.yaml"))
			}
		}
	}
	if exists(filepath.Join(d, "config.json")) {
		return "the config format changed to profiles in profiles.yaml, run `syno login` again"
	}
	return ""
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// LoadProfiles returns empty Profiles when the file does not exist yet.
func LoadProfiles() (*Profiles, error) {
	p, err := ProfilesPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Profiles{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Profiles
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return &c, nil
}

// Save writes profiles.yaml with permissions only the user can read.
func (c *Profiles) Save() error {
	p, err := ProfilesPath()
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
func (c *Profiles) Select(name string) (string, *Profile, error) {
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
func (c *Profiles) Set(name string, p *Profile) {
	if c.Profiles == nil {
		c.Profiles = map[string]*Profile{}
	}
	c.Profiles[name] = p
	if c.Current == "" {
		c.Current = name
	}
}

// Remove deletes a profile and clears Current if it pointed to it.
func (c *Profiles) Remove(name string) {
	delete(c.Profiles, name)
	if c.Current == name {
		c.Current = ""
	}
}

// FindByURL returns the name of the profile for url and user, if any.
func (c *Profiles) FindByURL(url, user string) (string, bool) {
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
func (c *Profiles) SharesAccount(name string) bool {
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
func (c *Profiles) Names() []string {
	names := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
