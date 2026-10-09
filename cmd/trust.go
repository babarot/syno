package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/babarot/syno/internal/dsm"
)

// trustOptions controls how login decides to trust a server certificate.
type trustOptions struct {
	// AllowHTTP permits a plain HTTP URL, which sends the password in clear.
	AllowHTTP bool
	// TrustPin is a pin given on the command line, for non-interactive use.
	TrustPin string
	// KnownPin is the pin already saved in the profile, if any.
	KnownPin string
}

// trustServer decides how to trust the certificate of rawURL before any
// password is sent. It returns the pin to save: empty when the certificate
// passes the usual verification.
func trustServer(ctx context.Context, rawURL string, opts trustOptions) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !opts.AllowHTTP {
			return "", fmt.Errorf("refusing to send the password over plain HTTP to %s; use the https URL (usually port 5001), or pass --allow-http", rawURL)
		}
		fmt.Fprintf(os.Stderr, "Warning: the password is sent in clear text to %s.\n", rawURL)
		return "", nil
	default:
		return "", fmt.Errorf("unsupported URL %s, want https://host:port", rawURL)
	}

	probe, err := dsm.ProbeTLS(ctx, rawURL, nil)
	if err != nil {
		return "", err
	}
	if probe.Verified() {
		return "", nil
	}

	pin := dsm.Pin(probe.Cert)
	switch {
	case opts.TrustPin != "":
		if opts.TrustPin != pin {
			return "", &dsm.PinMismatchError{URL: rawURL, Want: opts.TrustPin, Got: pin}
		}
		return pin, nil
	case opts.KnownPin == pin:
		return pin, nil
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("the certificate of %s is not trusted by this system (%v); "+
			"check it and pass --trust-pin %s", rawURL, probe.VerifyErr, pin)
	}
	ok, err := confirmTrust(os.Stdin, os.Stderr, rawURL, probe, opts.KnownPin)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("certificate not trusted, login aborted")
	}
	return pin, nil
}

// confirmTrust shows the certificate and asks whether to pin it.
func confirmTrust(in io.Reader, out io.Writer, rawURL string, probe *dsm.TLSProbe, knownPin string) (bool, error) {
	c := probe.Cert
	if knownPin != "" {
		fmt.Fprintf(out, "WARNING: the certificate of %s changed since the last login.\n", rawURL)
		fmt.Fprintf(out, "  Pinned:   %s\n", knownPin)
	} else {
		fmt.Fprintf(out, "The certificate of %s is not trusted by this system:\n", rawURL)
	}
	fmt.Fprintf(out, "  Reason:   %v\n", probe.VerifyErr)
	fmt.Fprintf(out, "  Subject:  %s\n", c.Subject)
	if len(c.DNSNames) > 0 {
		fmt.Fprintf(out, "  Names:    %s\n", strings.Join(c.DNSNames, ", "))
	}
	fmt.Fprintf(out, "  Issuer:   %s\n", c.Issuer)
	fmt.Fprintf(out, "  Expires:  %s\n", c.NotAfter.Format("2006-01-02"))
	fmt.Fprintf(out, "  Key:      %s\n", dsm.Pin(c))
	fmt.Fprint(out, "Trust this certificate and pin its public key? [y/N] ")

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
