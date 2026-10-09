package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"

	"github.com/babarot/syno/internal/credential"
	"github.com/babarot/syno/internal/dsm"
)

type fakeClient struct {
	sid      string
	valid    map[string]bool // sessions DSM accepts
	probeErr error           // overrides the answer to NeedReboot
	logins   int
	logouts  int
}

func (f *fakeClient) SID() string       { return f.sid }
func (f *fakeClient) SetSID(sid string) { f.sid = sid }
func (f *fakeClient) Login(context.Context, dsm.LoginOptions) (string, error) {
	f.logins++
	f.sid = "new-sid"
	return "", nil
}
func (f *fakeClient) Logout(context.Context) error {
	f.logouts++
	f.sid = ""
	return nil
}
func (f *fakeClient) NeedReboot(context.Context) (bool, error) {
	if f.probeErr != nil {
		return false, f.probeErr
	}
	if !f.valid[f.sid] {
		return false, &dsm.APIError{API: "SYNO.Core.Hardware.NeedReboot", Method: "get", Code: 119}
	}
	return false, nil
}

type fakeStore struct {
	session string
	saveErr error
	readErr error
}

func (f *fakeStore) Password(string, string) (string, error) { return "secret", nil }
func (f *fakeStore) DeviceID(string, string) (string, error) { return "", credential.ErrNotFound }
func (f *fakeStore) Session(string, string) (string, error) {
	if f.readErr != nil {
		return "", f.readErr
	}
	if f.session == "" {
		return "", credential.ErrNotFound
	}
	return f.session, nil
}
func (f *fakeStore) SetSession(_, _, sid string) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.session = sid
	return nil
}

func open(t *testing.T, c *fakeClient, s *fakeStore) (func(), error) {
	t.Helper()
	return openSession(context.Background(), c, s, "https://nas.example.com:5001", "admin")
}

func TestOpenSessionResumes(t *testing.T) {
	c := &fakeClient{valid: map[string]bool{"saved-sid": true}}
	s := &fakeStore{session: "saved-sid"}
	release, err := open(t, c, s)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if c.logins != 0 || c.logouts != 0 || c.sid != "saved-sid" {
		t.Errorf("logins=%d logouts=%d sid=%q, want the saved session resumed", c.logins, c.logouts, c.sid)
	}
}

func TestOpenSessionLogsInAgainWhenExpired(t *testing.T) {
	c := &fakeClient{valid: map[string]bool{}}
	s := &fakeStore{session: "expired-sid"}
	release, err := open(t, c, s)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if c.logins != 1 || c.logouts != 0 || s.session != "new-sid" {
		t.Errorf("logins=%d logouts=%d saved=%q, want a new login saved", c.logins, c.logouts, s.session)
	}
}

func TestOpenSessionLogsInWithoutSavedSession(t *testing.T) {
	for name, s := range map[string]*fakeStore{
		"none":             {},
		"keyring unusable": {readErr: errors.New("no keyring")},
	} {
		t.Run(name, func(t *testing.T) {
			c := &fakeClient{}
			if _, err := open(t, c, s); err != nil {
				t.Fatal(err)
			}
			if c.logins != 1 {
				t.Errorf("logins = %d, want 1", c.logins)
			}
		})
	}
}

func TestOpenSessionLogsOutWhenNotSaved(t *testing.T) {
	c := &fakeClient{}
	s := &fakeStore{saveErr: errors.New("no keyring")}
	release, err := open(t, c, s)
	if err != nil {
		t.Fatal(err)
	}
	if c.logouts != 0 {
		t.Fatal("logged out before the command ran")
	}
	release()
	if c.logouts != 1 {
		t.Errorf("logouts = %d, want the unsaved session logged out", c.logouts)
	}
}

func TestOpenSessionReturnsConnectionErrors(t *testing.T) {
	certErr := &tls.CertificateVerificationError{Err: errors.New("bad certificate")}
	c := &fakeClient{probeErr: certErr}
	s := &fakeStore{session: "saved-sid"}
	if _, err := open(t, c, s); !errors.Is(err, certErr) {
		t.Errorf("err = %v, want the certificate error", err)
	}
	if c.logins != 0 {
		t.Errorf("logins = %d, want no login with the password after a certificate error", c.logins)
	}
}
