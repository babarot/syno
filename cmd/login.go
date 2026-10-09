package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/babarot/syno/internal/config"
	"github.com/babarot/syno/internal/credential"
	"github.com/babarot/syno/internal/dsm"
)

const discoverTimeout = 3 * time.Second

func newLoginCmd() *cobra.Command {
	var host, user, otp string
	var trust trustOptions

	c := &cobra.Command{
		Use:   "login",
		Short: "Log in to a NAS and save it as a profile",
		Long: `Log in to DSM once to check the account, then save the NAS and the user as
a profile in ~/.config/syno/profiles.yaml and the password to the OS keyring.

Without --host, the NAS is found on the local network. Without --profile, the
profile is named after the NAS, and an existing profile for the same URL and
user is updated. The first profile becomes the current one.

Before the password is sent, the server certificate is checked. When the
system does not trust it, as with DSM's self-signed certificate or access by
IP address, its details are shown and you are asked whether to pin its public
key. Later commands then accept only that key.

Storage information needs an account in the administrators group. When the
account uses 2FA, you are asked for a code once and the device token DSM
issues is saved so that later commands do not ask again.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// Not loadProfiles: logging in is how users move off config.json.
			cfg, err := config.LoadProfiles()
			if err != nil {
				return err
			}

			name := profileName()
			var existing *config.Profile
			if name != "" {
				existing = cfg.Profiles[name]
			}

			var mdnsName string
			switch {
			case host != "":
			case existing != nil:
				host = existing.URL
			default:
				if host, mdnsName, err = discoverOne(ctx); err != nil {
					return err
				}
			}
			if user == "" && existing != nil {
				user = existing.User
			}
			if user == "" {
				if user, err = prompt("User: ", false); err != nil {
					return err
				}
			}
			if name == "" {
				if n, ok := cfg.FindByURL(host, user); ok {
					name = n
				} else {
					name = defaultProfileName(mdnsName, host)
				}
			}

			// Settle the certificate before the password leaves this machine.
			if existing := cfg.Profiles[name]; existing != nil && existing.URL == host {
				trust.KnownPin = existing.TLS.Pin
			}
			pin, err := trustServer(ctx, host, trust)
			if err != nil {
				return err
			}

			password, err := prompt(fmt.Sprintf("Password for %s@%s: ", user, host), true)
			if err != nil {
				return err
			}

			client := dsm.New(host, pin)
			opts := dsm.LoginOptions{User: user, Password: password, OTP: otp}
			deviceID, err := client.Login(ctx, opts)
			if dsm.IsOTPRequired(err) && otp == "" {
				if opts.OTP, err = prompt("2FA code: ", false); err != nil {
					return err
				}
				deviceID, err = client.Login(ctx, opts)
			}
			if err != nil {
				return err
			}
			// Log out unless the session gets saved below, so that a failure
			// in between does not leave it on the NAS.
			release := func() { _ = client.Logout(ctx) }
			defer func() { release() }()

			if err := credential.SetPassword(host, user, password); err != nil {
				return err
			}
			if deviceID != "" {
				if err := credential.SetDeviceID(host, user, deviceID); err != nil {
					return err
				}
			}
			cfg.Set(name, &config.Profile{URL: host, User: user, TLS: config.TLS{Pin: pin}, MACs: wakeMACs(ctx, client)})
			if err := cfg.Save(); err != nil {
				return err
			}
			// Later commands resume this session instead of logging in again.
			release = saveSession(ctx, client, keyringStore{}, host, user)
			fmt.Fprintf(os.Stderr, "Logged in to %s as %s, saved as profile %q.\n", host, user, name)
			return nil
		},
	}

	c.Flags().StringVar(&host, "host", "", "DSM URL, e.g. https://192.168.1.10:5001 (default: discover)")
	c.Flags().StringVarP(&user, "user", "u", "", "DSM account name")
	c.Flags().StringVar(&otp, "otp", "", "2FA code (asked interactively when needed)")
	c.Flags().StringVar(&trust.TrustPin, "trust-pin", "", "Trust a certificate whose public key has this pin (sha256/...) without asking")
	c.Flags().BoolVar(&trust.AllowHTTP, "allow-http", false, "Allow logging in over plain HTTP, sending the password in clear text")

	return c
}

// prompt reads one line from the terminal, hiding the input when secret.
// When stdin is not a terminal it reads a line from stdin instead.
func prompt(label string, secret bool) (string, error) {
	fmt.Fprint(os.Stderr, label)
	fd := int(os.Stdin.Fd())
	if secret && term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	if line == "" && err != nil {
		return "", errors.New("no input")
	}
	return line, nil
}

// wakeMACs returns the MAC addresses syno wake needs later, when the NAS is
// off and cannot be asked. It also tells when Wake-on-LAN is off in DSM.
// Neither stops the login: syno wake can take --mac.
func wakeMACs(ctx context.Context, c *dsm.Client) []string {
	nifs, err := c.NetworkInterfaces(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not read the MAC addresses for syno wake: %v\n", err)
		return nil
	}
	var macs []string
	for _, n := range nifs {
		if mac, err := net.ParseMAC(n.MAC); err == nil && len(mac) == 6 && !slices.Contains(macs, mac.String()) {
			macs = append(macs, mac.String())
		}
	}
	if on, err := c.WakeOnLANEnabled(ctx); err == nil && !on {
		fmt.Fprintln(os.Stderr, "Note: Wake-on-LAN is off in DSM, so syno wake cannot start this NAS. Turn it on in Control Panel > Hardware & Power.")
	}
	return macs
}
