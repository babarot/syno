package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/babarot/syno/internal/doctor"
)

func writeConfigYAML(t *testing.T, content string) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", d)
	if content == "" {
		return
	}
	if err := os.MkdirAll(filepath.Join(d, "syno"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "syno", "config.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorOptionsDefaults(t *testing.T) {
	writeConfigYAML(t, "")
	opts, err := doctorOptions(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Thresholds != doctor.DefaultThresholds() {
		t.Errorf("thresholds = %+v, want the defaults", opts.Thresholds)
	}
	if len(opts.Skip) != 0 || len(opts.Only) != 0 {
		t.Errorf("skip %v, only %v", opts.Skip, opts.Only)
	}
}

func TestDoctorOptionsThresholds(t *testing.T) {
	writeConfigYAML(t, `doctor:
  thresholds:
    volume_usage: {warn: 80}
    scrub_age_days: 30
`)
	opts, err := doctorOptions(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := doctor.DefaultThresholds()
	want.VolumeUsageWarn = 80
	want.ScrubAgeWarn = 30 * 24 * time.Hour
	if opts.Thresholds != want {
		t.Errorf("thresholds = %+v, want %+v", opts.Thresholds, want)
	}
}

func TestDoctorOptionsInvalidThresholds(t *testing.T) {
	writeConfigYAML(t, `doctor:
  thresholds:
    volume_usage: {warn: 96}
    cert_expiry_days: 0
`)
	_, err := doctorOptions(nil, nil)
	if err == nil {
		t.Fatal("want an error")
	}
	for _, s := range []string{"volume_usage", "warn (96) < fail (95)", "cert_expiry_days"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not mention %q", err, s)
		}
	}
}

func TestDoctorOptionsSelection(t *testing.T) {
	writeConfigYAML(t, "doctor:\n  skip: [dsm-update]\n")

	opts, err := doctorOptions([]string{"certificates"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(opts.Skip, []string{"dsm-update", "certificates"}) {
		t.Errorf("skip = %v, want the config and the flag combined", opts.Skip)
	}

	opts, err = doctorOptions(nil, []string{"volumes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(opts.Skip) != 0 || !slices.Equal(opts.Only, []string{"volumes"}) {
		t.Errorf("--only: skip %v, only %v; want the config skip ignored", opts.Skip, opts.Only)
	}

	if _, err := doctorOptions([]string{"nope"}, nil); err == nil || !strings.Contains(err.Error(), "--skip") {
		t.Errorf("unknown --skip: %v", err)
	}
	if _, err := doctorOptions(nil, []string{"nope"}); err == nil || !strings.Contains(err.Error(), "--only") {
		t.Errorf("unknown --only: %v", err)
	}

	writeConfigYAML(t, "doctor:\n  skip: [nope]\n")
	if _, err := doctorOptions(nil, nil); err == nil || !strings.Contains(err.Error(), "doctor.skip") {
		t.Errorf("unknown name in config: %v", err)
	}
}
