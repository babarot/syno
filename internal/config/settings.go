package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/goccy/go-yaml"
)

// Settings is the content of config.yaml. Users write this file; syno only
// reads it, so comments in it are kept.
type Settings struct {
	Doctor DoctorSettings `yaml:"doctor"`
}

// DoctorSettings configures syno doctor.
type DoctorSettings struct {
	// Skip names checks to turn off.
	Skip       []string         `yaml:"skip"`
	Thresholds DoctorThresholds `yaml:"thresholds"`
}

// DoctorThresholds overrides the doctor thresholds. Nil fields keep the
// defaults.
type DoctorThresholds struct {
	VolumeUsage             Range `yaml:"volume_usage"`     // percent
	DiskTemperature         Range `yaml:"disk_temperature"` // Celsius
	ScrubAgeDays            *int  `yaml:"scrub_age_days"`
	SecurityScanAgeDays     *int  `yaml:"security_scan_age_days"`
	CertExpiryDays          *int  `yaml:"cert_expiry_days"`
	RenewableCertExpiryDays *int  `yaml:"renewable_cert_expiry_days"`
}

// Range is a pair of warn and fail levels.
type Range struct {
	Warn *float64 `yaml:"warn"`
	Fail *float64 `yaml:"fail"`
}

// SettingsPath returns the location of config.yaml.
func SettingsPath() (string, error) { return path("config.yaml") }

// LoadSettings returns empty Settings when config.yaml does not exist.
// Unknown keys are errors, so that typos do not go unnoticed.
func LoadSettings() (*Settings, error) {
	p, err := SettingsPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Settings{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s Settings
	if err := yaml.UnmarshalWithOptions(b, &s, yaml.Strict()); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return &s, nil
}
