package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/babarot/syno/internal/config"
	"github.com/babarot/syno/internal/credential"
	"github.com/babarot/syno/internal/discover"
	"github.com/babarot/syno/internal/dsm"
)

// resolveHost picks the DSM base URL: the flag, then the config file, then
// discovery when exactly one NAS answers.
func resolveHost(ctx context.Context, flag string, cfg *config.Config) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if cfg.Host != "" {
		return cfg.Host, nil
	}
	devices, err := discover.Run(ctx, discover.Options{Timeout: discoverTimeout})
	if err != nil {
		return "", err
	}
	var found []string
	for _, d := range devices {
		if d.DSMURL != "" {
			found = append(found, d.DSMURL)
		}
	}
	switch len(found) {
	case 0:
		return "", errors.New("no DSM found on the network, pass --host")
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("found %d DSM hosts %v, pick one with --host", len(found), found)
	}
}

// connect logs in with the account saved by `syno login`.
// SYNO_PASSWORD overrides the keychain, which helps on non-macOS hosts.
func connect(ctx context.Context, hostFlag string) (*dsm.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if cfg.User == "" {
		return nil, errors.New("no account configured, run `syno login` first")
	}
	host, err := resolveHost(ctx, hostFlag, cfg)
	if err != nil {
		return nil, err
	}

	password := os.Getenv("SYNO_PASSWORD")
	if password == "" {
		password, err = credential.Password(host, cfg.User)
		if errors.Is(err, credential.ErrNotFound) {
			return nil, fmt.Errorf("no password saved for %s@%s, run `syno login`", cfg.User, host)
		}
		if err != nil {
			return nil, err
		}
	}
	deviceID, err := credential.DeviceID(host, cfg.User)
	if err != nil && !errors.Is(err, credential.ErrNotFound) {
		return nil, err
	}

	c := dsm.New(host)
	if _, err := c.Login(ctx, dsm.LoginOptions{User: cfg.User, Password: password, DeviceID: deviceID}); err != nil {
		return nil, err
	}
	return c, nil
}
