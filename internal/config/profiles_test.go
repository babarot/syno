package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveLoad(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	c, err := LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Profiles) != 0 {
		t.Fatalf("empty config has profiles: %v", c.Profiles)
	}

	c.Set("home", &Profile{URL: "https://192.168.1.10:5001", User: "admin", TLS: TLS{Pin: "sha256/abc"}})
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	p, _ := ProfilesPath()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permission = %o, want 600", perm)
	}

	got, err := LoadProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if got.Current != "home" {
		t.Errorf("Current = %q, want home", got.Current)
	}
	if prof := got.Profiles["home"]; prof == nil || prof.URL != "https://192.168.1.10:5001" || prof.User != "admin" || prof.TLS.Pin != "sha256/abc" {
		t.Errorf("profile = %+v", prof)
	}
}

func TestSelect(t *testing.T) {
	one := &Profiles{Profiles: map[string]*Profile{"a": {URL: "https://a"}}}
	if name, _, err := one.Select(""); err != nil || name != "a" {
		t.Errorf("single profile: %q, %v", name, err)
	}

	two := &Profiles{Current: "b", Profiles: map[string]*Profile{"a": {URL: "https://a"}, "b": {URL: "https://b"}}}
	if name, _, err := two.Select(""); err != nil || name != "b" {
		t.Errorf("current: %q, %v", name, err)
	}
	if name, _, err := two.Select("a"); err != nil || name != "a" {
		t.Errorf("explicit: %q, %v", name, err)
	}
	if _, _, err := two.Select("nope"); err == nil {
		t.Error("unknown profile should fail")
	}

	two.Current = ""
	if _, _, err := two.Select(""); err == nil {
		t.Error("ambiguous selection should fail")
	}

	if _, _, err := (&Profiles{}).Select(""); !errors.Is(err, ErrNoProfile) {
		t.Errorf("empty: err = %v, want ErrNoProfile", err)
	}
}

func TestRemoveAndSharesAccount(t *testing.T) {
	c := &Profiles{}
	c.Set("a", &Profile{URL: "https://nas", User: "admin"})
	c.Set("b", &Profile{URL: "https://nas", User: "admin"})
	c.Set("c", &Profile{URL: "https://nas", User: "other"})

	if !c.SharesAccount("a") {
		t.Error("a shares its account with b")
	}
	if c.SharesAccount("c") {
		t.Error("c shares its account with nobody")
	}

	c.Remove("a")
	if c.Current != "" {
		t.Errorf("Current = %q after removing it", c.Current)
	}
	if c.SharesAccount("b") {
		t.Error("b no longer shares its account")
	}
}

func TestLegacyHint(t *testing.T) {
	d := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", d)
	dir := filepath.Join(d, "syno")
	write := func(name, content string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if got := LegacyHint(); got != "" {
		t.Errorf("no files: %q", got)
	}

	write("config.json", "{}")
	if got := LegacyHint(); !strings.Contains(got, "syno login") {
		t.Errorf("config.json: %q", got)
	}

	write("config.yaml", "profiles:\n  nas:\n    url: https://192.168.1.10:5001\n")
	if got := LegacyHint(); !strings.Contains(got, "rename") {
		t.Errorf("config.yaml with profiles: %q", got)
	}

	write("config.yaml", "doctor:\n  skip: []\n")
	if got := LegacyHint(); !strings.Contains(got, "syno login") {
		t.Errorf("settings-only config.yaml and config.json: %q", got)
	}

	if err := (&Profiles{}).Save(); err != nil {
		t.Fatal(err)
	}
	if got := LegacyHint(); got != "" {
		t.Errorf("profiles.yaml exists: %q", got)
	}
}
