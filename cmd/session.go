package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

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

// connect returns a client logged in with the selected profile. It resumes
// the session saved in the keyring by an earlier command when DSM still
// accepts it, and logs in with the password otherwise. Commands defer the
// returned release, which logs out only a session that could not be saved.
func connect(ctx context.Context) (*dsm.Client, func(), error) {
	_, p, err := selectProfile()
	if err != nil {
		return nil, nil, err
	}
	c := dsm.New(p.URL, p.TLS.Pin)
	release, err := openSession(ctx, c, keyringStore{}, p.URL, p.User)
	if err != nil {
		return nil, nil, explainConnectError(err)
	}
	return c, release, nil
}

// explainConnectError tells how to fix a certificate DSM no longer matches.
func explainConnectError(err error) error {
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return fmt.Errorf("%w; run `syno login` to check the certificate and pin it", err)
	}
	return err
}

// loginTimeout bounds logging in again for a long-running command, which
// does not end with the request that noticed the session was gone.
const loginTimeout = 30 * time.Second

// liveSession keeps one client logged in to a profile for a long-running
// command, such as syno mcp, instead of opening a session for every
// request. It logs in again when DSM drops the session.
type liveSession struct {
	url, user string
	client    *dsm.Client
	store     secretStore
	login     singleflight.Group

	mu      sync.Mutex
	opened  bool
	release func()
}

func newLiveSession(p *config.Profile, store secretStore) *liveSession {
	return &liveSession{url: p.URL, user: p.User, client: dsm.New(p.URL, p.TLS.Pin), store: store}
}

// do runs fn with the client, opening the session on first use. When DSM
// says during fn that the session is gone (106, 107, 119), it logs in
// again, once for all the callers that noticed it together, and runs fn
// once more. fn may have to run twice, so it must only read.
func (s *liveSession) do(ctx context.Context, fn func(*dsm.Client) error) error {
	if !s.isOpen() {
		if err := s.open(ctx, ""); err != nil {
			return err
		}
	}
	sid, lost := s.client.SID(), s.client.SessionLosses()
	err := fn(s.client)
	if !dsm.IsSessionGone(err) && s.client.SessionLosses() == lost {
		return err
	}
	if err := s.open(ctx, sid); err != nil {
		return err
	}
	return fn(s.client)
}

func (s *liveSession) isOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opened
}

// open opens the session, or replaces stale, the session a caller found
// gone. Callers that arrive together share one login, and a caller whose
// stale session was already replaced does not log in again.
func (s *liveSession) open(ctx context.Context, stale string) error {
	_, err, _ := s.login.Do("open", func() (any, error) {
		if s.isOpen() && s.client.SID() != stale {
			return nil, nil
		}
		// The login outlives the request that asked for it, since the
		// callers waiting for it may have more time.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loginTimeout)
		defer cancel()
		release, err := openSession(ctx, s.client, s.store, s.url, s.user)
		if err != nil {
			return nil, explainConnectError(err)
		}
		s.mu.Lock()
		s.opened, s.release = true, release
		s.mu.Unlock()
		return nil, nil
	})
	return err
}

// close logs out a session that could not be saved, as connect's release
// does when a command ends.
func (s *liveSession) close() {
	s.mu.Lock()
	release := s.release
	s.mu.Unlock()
	if release != nil {
		release()
	}
}

// sessionClient is the part of dsm.Client that openSession uses.
type sessionClient interface {
	SID() string
	SetSID(sid string)
	Login(ctx context.Context, o dsm.LoginOptions) (string, error)
	Logout(ctx context.Context) error
	NeedReboot(ctx context.Context) (bool, error)
}

// secretStore is the part of the keyring that openSession uses.
type secretStore interface {
	Password(host, user string) (string, error)
	DeviceID(host, user string) (string, error)
	Session(host, user string) (string, error)
	SetSession(host, user, sid string) error
}

// keyringStore reads the OS keyring. SYNO_PASSWORD overrides the saved
// password, for hosts without a keyring.
type keyringStore struct{}

func (keyringStore) Password(host, user string) (string, error) {
	if pw := os.Getenv("SYNO_PASSWORD"); pw != "" {
		return pw, nil
	}
	return credential.Password(host, user)
}
func (keyringStore) DeviceID(host, user string) (string, error) {
	return credential.DeviceID(host, user)
}
func (keyringStore) Session(host, user string) (string, error) { return credential.Session(host, user) }
func (keyringStore) SetSession(host, user, sid string) error {
	return credential.SetSession(host, user, sid)
}

func openSession(ctx context.Context, c sessionClient, store secretStore, host, user string) (release func(), err error) {
	if sid, err := store.Session(host, user); err == nil && sid != "" {
		c.SetSID(sid)
		// A cheap call tells whether DSM still accepts the session, and
		// surfaces certificate and network errors here rather than in the
		// middle of a command.
		_, err := c.NeedReboot(ctx)
		var apiErr *dsm.APIError
		switch {
		case err == nil:
			return func() {}, nil
		case errors.As(err, &apiErr):
			// An expired session, or an answer that says nothing about the
			// session (105 means it is valid but lacks permission): log in
			// again either way.
			c.SetSID("")
		default:
			return nil, err
		}
	}

	password, err := store.Password(host, user)
	if errors.Is(err, credential.ErrNotFound) {
		return nil, fmt.Errorf("no password saved for %s@%s, run `syno login`", user, host)
	}
	if err != nil {
		return nil, err
	}
	deviceID, err := store.DeviceID(host, user)
	if err != nil && !errors.Is(err, credential.ErrNotFound) {
		return nil, err
	}
	if _, err := c.Login(ctx, dsm.LoginOptions{User: user, Password: password, DeviceID: deviceID}); err != nil {
		return nil, err
	}
	return saveSession(ctx, c, store, host, user), nil
}

// saveSession keeps the session for later commands and returns what to do
// when the command ends. A session that cannot be saved, as on a host
// without a keyring, is logged out then rather than left on the NAS.
func saveSession(ctx context.Context, c sessionClient, store secretStore, host, user string) (release func()) {
	if err := store.SetSession(host, user, c.SID()); err != nil {
		// The command's context may be canceled by then, by Ctrl-C or by
		// the end of the login of a long-running command.
		return func() { _ = c.Logout(context.WithoutCancel(ctx)) }
	}
	return func() {}
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
