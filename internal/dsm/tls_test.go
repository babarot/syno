package dsm

import (
	"context"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTLSServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"need_reboot":false}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPinnedClient(t *testing.T) {
	srv := newTLSServer(t)
	ctx := context.Background()

	if _, err := New(srv.URL, Pin(srv.Certificate())).NeedReboot(ctx); err != nil {
		t.Errorf("matching pin: %v", err)
	}

	_, err := New(srv.URL, "sha256/AAAA").NeedReboot(ctx)
	var mismatch *PinMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("wrong pin: err = %v, want PinMismatchError", err)
	}
	if mismatch.Got != Pin(srv.Certificate()) {
		t.Errorf("mismatch reports %s, want the server's pin", mismatch.Got)
	}

	// The test server's CA is not in the system roots.
	var unknown x509.UnknownAuthorityError
	if _, err := New(srv.URL, "").NeedReboot(ctx); !errors.As(err, &unknown) {
		t.Errorf("no pin: err = %v, want an unknown authority error", err)
	}

	if _, err := NewInsecure(srv.URL).NeedReboot(ctx); err != nil {
		t.Errorf("insecure: %v", err)
	}
}

func TestProbeTLS(t *testing.T) {
	srv := newTLSServer(t)
	ctx := context.Background()

	probe, err := ProbeTLS(ctx, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Verified() {
		t.Error("certificate from an unknown CA should not verify")
	}
	if Pin(probe.Cert) != Pin(srv.Certificate()) {
		t.Error("probe returned another certificate")
	}

	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	probe, err = ProbeTLS(ctx, srv.URL, roots)
	if err != nil {
		t.Fatal(err)
	}
	if !probe.Verified() {
		t.Errorf("certificate from a trusted CA should verify: %v", probe.VerifyErr)
	}

	if _, err := ProbeTLS(ctx, "http://127.0.0.1:1", nil); err == nil {
		t.Error("http URL should be rejected")
	}
}
