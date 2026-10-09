package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/babarot/syno/internal/credential"
	"github.com/babarot/syno/internal/dsm"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "End the saved session of the profile",
		Long: `End the DSM session that commands of the profile share, and forget it.
The password and the 2FA device token stay saved, so the next command logs
in again by itself. Use syno profile remove to forget them too.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, p, err := selectProfile()
			if err != nil {
				return err
			}
			sid, err := credential.Session(p.URL, p.User)
			if errors.Is(err, credential.ErrNotFound) {
				fmt.Fprintf(os.Stderr, "No session saved for %s@%s.\n", p.User, p.URL)
				return nil
			}
			if err != nil {
				return err
			}

			c := dsm.New(p.URL, p.TLS.Pin)
			c.SetSID(sid)
			// The saved session is forgotten even when DSM cannot be reached;
			// it then expires on the NAS by itself.
			logoutErr := c.Logout(cmd.Context())
			if err := credential.DeleteSession(p.URL, p.User); err != nil {
				return err
			}
			if logoutErr != nil {
				fmt.Fprintf(os.Stderr, "Forgot the session, but could not log it out: %v\n", logoutErr)
				return nil
			}
			fmt.Fprintf(os.Stderr, "Logged out of %s as %s.\n", p.URL, p.User)
			return nil
		},
	}
}
