package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
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

	c := &cobra.Command{
		Use:   "login",
		Short: "Save a DSM account to use with other commands",
		Long: `Log in to DSM once to check the account, then save the host and user to
~/.config/syno/config.json and the password to the macOS keychain.

Storage information needs an account in the administrators group. When the
account uses 2FA, you are asked for a code once and the device token DSM
issues is saved so that later commands do not ask again.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if host, err = resolveHost(ctx, host, cfg); err != nil {
				return err
			}
			if user == "" {
				user = cfg.User
			}
			if user == "" {
				if user, err = prompt("User: ", false); err != nil {
					return err
				}
			}
			password, err := prompt(fmt.Sprintf("Password for %s@%s: ", user, host), true)
			if err != nil {
				return err
			}

			client := dsm.New(host)
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
			_ = client.Logout(ctx)

			if err := credential.SetPassword(host, user, password); err != nil {
				return err
			}
			if deviceID != "" {
				if err := credential.SetDeviceID(host, user, deviceID); err != nil {
					return err
				}
			}
			cfg.Host, cfg.User = host, user
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Logged in to %s as %s.\n", host, user)
			return nil
		},
	}

	c.Flags().StringVar(&host, "host", "", "DSM URL, e.g. https://192.168.1.10:5001 (default: discover)")
	c.Flags().StringVarP(&user, "user", "u", "", "DSM account name")
	c.Flags().StringVar(&otp, "otp", "", "2FA code (asked interactively when needed)")

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
