package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSettings(t *testing.T, content string) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", d)
	if err := os.MkdirAll(filepath.Join(d, "syno"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "syno", "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSettings(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, err := LoadSettings()
	if err != nil || s.Doctor.Skip != nil || s.Doctor.Thresholds.VolumeUsage.Warn != nil {
		t.Errorf("missing file: %+v, %v", s, err)
	}

	writeSettings(t, `# doctor settings
doctor:
  skip: [dsm-update]
  thresholds:
    volume_usage: {warn: 80}
    scrub_age_days: 30
`)
	s, err = LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	th := s.Doctor.Thresholds
	if len(s.Doctor.Skip) != 1 || s.Doctor.Skip[0] != "dsm-update" {
		t.Errorf("skip = %v", s.Doctor.Skip)
	}
	if th.VolumeUsage.Warn == nil || *th.VolumeUsage.Warn != 80 || th.VolumeUsage.Fail != nil {
		t.Errorf("volume_usage = %+v", th.VolumeUsage)
	}
	if th.ScrubAgeDays == nil || *th.ScrubAgeDays != 30 || th.CertExpiryDays != nil {
		t.Errorf("days: scrub %v, cert %v", th.ScrubAgeDays, th.CertExpiryDays)
	}
}

func TestLoadSettingsRejectsUnknownKeys(t *testing.T) {
	writeSettings(t, "doctor:\n  thresholds:\n    volume_usgae: {warn: 80}\n")
	_, err := LoadSettings()
	if err == nil || !strings.Contains(err.Error(), "volume_usgae") {
		t.Errorf("err = %v, want one naming the unknown key", err)
	}
}
