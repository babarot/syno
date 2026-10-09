package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/babarot/syno/internal/credential"
)

func newProfileCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "profile",
		Short: "Manage the saved NAS profiles",
	}
	c.AddCommand(
		&cobra.Command{
			Use:     "list",
			Aliases: []string{"ls"},
			Short:   "List the profiles, marking the current one",
			Args:    cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadProfiles()
				if err != nil {
					return err
				}
				if len(cfg.Profiles) == 0 {
					fmt.Fprintln(os.Stderr, "No profiles. Run `syno login` to add one.")
					return nil
				}
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
				fmt.Fprintln(w, "\tNAME\tURL\tUSER\tTLS")
				for _, name := range cfg.Names() {
					p := cfg.Profiles[name]
					mark := ""
					if name == cfg.Current {
						mark = "*"
					}
					tls := "system"
					switch {
					case p.TLS.Pin != "":
						tls = "pinned"
					case strings.HasPrefix(p.URL, "http://"):
						tls = "none"
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", mark, name, p.URL, p.User, tls)
				}
				return w.Flush()
			},
		},
		&cobra.Command{
			Use:   "use <name>",
			Short: "Make a profile the current one",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := loadProfiles()
				if err != nil {
					return err
				}
				if _, _, err := cfg.Select(args[0]); err != nil {
					return err
				}
				cfg.Current = args[0]
				return cfg.Save()
			},
		},
		&cobra.Command{
			Use:     "remove <name>",
			Aliases: []string{"rm"},
			Short:   "Remove a profile and its saved credentials",
			Long: `Remove a profile from the config, and its password and device token from the
OS keyring. The keyring items are kept when another profile uses the same URL
and user.`,
			Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				name := args[0]
				cfg, err := loadProfiles()
				if err != nil {
					return err
				}
				_, p, err := cfg.Select(name)
				if err != nil {
					return err
				}
				if !cfg.SharesAccount(name) {
					if err := credential.Delete(p.URL, p.User); err != nil {
						return err
					}
				}
				cfg.Remove(name)
				if err := cfg.Save(); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Removed profile %q.\n", name)
				return nil
			},
		},
	)
	return c
}
