package dsm

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
)

// Pin returns the SHA-256 of the certificate's public key (SPKI) as
// "sha256/<base64>". Pinning the key rather than the whole certificate
// keeps the pin valid across renewals that reuse the key.
func Pin(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return "sha256/" + base64.StdEncoding.EncodeToString(sum[:])
}

// PinMismatchError means the server presented a different public key than
// the pinned one.
type PinMismatchError struct {
	URL  string
	Want string
	Got  string
}

func (e *PinMismatchError) Error() string {
	return fmt.Sprintf("certificate of %s changed (pinned %s, got %s). "+
		"If the certificate on the NAS was replaced, run `syno login` to trust the new one", e.URL, e.Want, e.Got)
}

// pinnedConfig skips the usual chain and host name checks and accepts the
// connection only when the leaf certificate carries the pinned key.
func pinnedConfig(base, pin string) *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("server sent no certificate")
			}
			if got := Pin(cs.PeerCertificates[0]); got != pin {
				return &PinMismatchError{URL: base, Want: pin, Got: got}
			}
			return nil
		},
	}
}

// TLSProbe is what ProbeTLS learned about a server.
type TLSProbe struct {
	// Cert is the leaf certificate the server presented.
	Cert *x509.Certificate
	// VerifyErr is why the usual verification failed, or nil if it passed.
	VerifyErr error
}

// Verified reports whether the certificate passed the usual verification.
func (p *TLSProbe) Verified() bool { return p.VerifyErr == nil }

// ProbeTLS connects to the HTTPS URL base and checks its certificate
// against roots (the system roots when nil) and the host name. When that
// fails, it connects again without verification to get the certificate, so
// that the caller can show it to the user and pin it.
func ProbeTLS(ctx context.Context, base string, roots *x509.CertPool) (*TLSProbe, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("%s is not an https URL", base)
	}
	addr := u.Host
	if u.Port() == "" {
		addr = net.JoinHostPort(u.Hostname(), "443")
	}

	dial := func(cfg *tls.Config) (*x509.Certificate, error) {
		d := tls.Dialer{Config: cfg}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
		if len(certs) == 0 {
			return nil, errors.New("server sent no certificate")
		}
		return certs[0], nil
	}

	cert, verifyErr := dial(&tls.Config{ServerName: u.Hostname(), RootCAs: roots})
	if verifyErr == nil {
		return &TLSProbe{Cert: cert}, nil
	}
	var certErr *tls.CertificateVerificationError
	if !errors.As(verifyErr, &certErr) {
		return nil, verifyErr // not a certificate problem, e.g. connection refused
	}
	cert, err = dial(&tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return nil, err
	}
	return &TLSProbe{Cert: cert, VerifyErr: certErr.Err}, nil
}
