package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/babarot/syno/internal/config"
	"github.com/babarot/syno/internal/credential"
	"github.com/babarot/syno/internal/discover"
	"github.com/babarot/syno/internal/dsm"
)

// profileFlag is the global --profile flag.
var profileFlag string

// profileName returns the profile asked for by --profile or SYNO_PROFILE,
// or "" to let the config decide.
func profileName() string {
	if profileFlag != "" {
		return profileFlag
	}
	return os.Getenv("SYNO_PROFILE")
}

// loadProfiles loads the profiles and explains the format change to users
// who still have the files of earlier versions.
func loadProfiles() (*config.Profiles, error) {
	cfg, err := config.LoadProfiles()
	if err != nil {
		return nil, err
	}
	if len(cfg.Profiles) == 0 {
		if hint := config.LegacyHint(); hint != "" {
			return nil, errors.New(hint)
		}
	}
	return cfg, nil
}

// selectProfile returns the profile to use for commands that log in.
func selectProfile() (string, *config.Profile, error) {
	cfg, err := loadProfiles()
	if err != nil {
		return "", nil, err
	}
	return cfg.Select(profileName())
}

// discoverOne finds the DSM on the network when exactly one answers, and
// returns its URL and mDNS name.
func discoverOne(ctx context.Context) (string, string, error) {
	devices, err := discover.Run(ctx, discover.Options{Timeout: discoverTimeout})
	if err != nil {
		return "", "", err
	}
	var found []discover.Device
	for _, d := range devices {
		if d.DSMURL != "" {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		return "", "", errors.New("no DSM found on the network, pass --host")
	case 1:
		return found[0].DSMURL, found[0].Name, nil
	default:
		var urls []string
		for _, d := range found {
			urls = append(urls, d.DSMURL)
		}
		return "", "", fmt.Errorf("found %d DSM hosts %v, pick one with --host", len(found), urls)
	}
}

// connect logs in with the selected profile.
// SYNO_PASSWORD overrides the keyring, for hosts without one.
func connect(ctx context.Context) (*dsm.Client, error) {
	_, p, err := selectProfile()
	if err != nil {
		return nil, err
	}

	password := os.Getenv("SYNO_PASSWORD")
	if password == "" {
		password, err = credential.Password(p.URL, p.User)
		if errors.Is(err, credential.ErrNotFound) {
			return nil, fmt.Errorf("no password saved for %s@%s, run `syno login`", p.User, p.URL)
		}
		if err != nil {
			return nil, err
		}
	}
	deviceID, err := credential.DeviceID(p.URL, p.User)
	if err != nil && !errors.Is(err, credential.ErrNotFound) {
		return nil, err
	}

	c := dsm.New(p.URL, p.TLS.Pin)
	if _, err := c.Login(ctx, dsm.LoginOptions{User: p.User, Password: password, DeviceID: deviceID}); err != nil {
		if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
			return nil, fmt.Errorf("%w; run `syno login` to check the certificate and pin it", err)
		}
		return nil, err
	}
	return c, nil
}

// defaultProfileName names a new profile after the NAS: its mDNS name, or
// the host part of its URL.
func defaultProfileName(mdnsName, rawURL string) string {
	if mdnsName != "" {
		return mdnsName
	}
	if u, err := url.Parse(rawURL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return "default"
}

// logout ends the session. A failure only leaves the session to expire on
// the NAS, so it is not worth failing a command that already did its work.
func logout(ctx context.Context, c *dsm.Client) {
	_ = c.Logout(ctx)
}
