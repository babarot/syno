package cmd

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/babarot/syno/internal/dsm"
)

// Tests run with stdin that is not a terminal, so trustServer never prompts.
func TestTrustServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	ctx := context.Background()
	pin := dsm.Pin(srv.Certificate())

	got, err := trustServer(ctx, srv.URL, trustOptions{TrustPin: pin})
	if err != nil || got != pin {
		t.Errorf("matching --trust-pin: %q, %v", got, err)
	}

	_, err = trustServer(ctx, srv.URL, trustOptions{TrustPin: "sha256/AAAA"})
	if _, ok := errors.AsType[*dsm.PinMismatchError](err); !ok {
		t.Errorf("wrong --trust-pin: err = %v, want PinMismatchError", err)
	}

	got, err = trustServer(ctx, srv.URL, trustOptions{KnownPin: pin})
	if err != nil || got != pin {
		t.Errorf("known pin: %q, %v", got, err)
	}

	_, err = trustServer(ctx, srv.URL, trustOptions{})
	if err == nil || !strings.Contains(err.Error(), "--trust-pin "+pin) {
		t.Errorf("no terminal: err = %v, want a hint with the pin", err)
	}

	_, err = trustServer(ctx, "http://192.168.1.10:5000", trustOptions{})
	if err == nil || !strings.Contains(err.Error(), "--allow-http") {
		t.Errorf("http: err = %v, want a refusal", err)
	}
	if got, err := trustServer(ctx, "http://192.168.1.10:5000", trustOptions{AllowHTTP: true}); err != nil || got != "" {
		t.Errorf("http with --allow-http: %q, %v", got, err)
	}
}

func TestConfirmTrust(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	probe := &dsm.TLSProbe{Cert: srv.Certificate(), VerifyErr: x509.UnknownAuthorityError{}}

	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"\n", false},
		{"anything\n", false},
	}
	for _, tt := range tests {
		var out bytes.Buffer
		got, err := confirmTrust(strings.NewReader(tt.input), &out, srv.URL, probe, "")
		if err != nil || got != tt.want {
			t.Errorf("input %q: %v, %v, want %v", tt.input, got, err, tt.want)
		}
		if !strings.Contains(out.String(), dsm.Pin(srv.Certificate())) {
			t.Errorf("input %q: prompt does not show the key pin:\n%s", tt.input, out.String())
		}
	}

	var out bytes.Buffer
	if _, err := confirmTrust(strings.NewReader("n\n"), &out, srv.URL, probe, "sha256/OLD"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "changed since the last login") || !strings.Contains(out.String(), "sha256/OLD") {
		t.Errorf("changed certificate is not called out:\n%s", out.String())
	}
}
