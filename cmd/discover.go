package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/babarot/syno/internal/discover"
)

func newDiscoverCmd() *cobra.Command {
	var opts discover.Options
	var asJSON bool

	c := &cobra.Command{
		Use:     "discover",
		Aliases: []string{"find", "ls"},
		Short:   "Discover Synology NAS on the local network",
		Long: `Discover Synology NAS on the local network.

Candidates are collected from mDNS, the ARP table (filtered by Synology's MAC
vendor prefixes) and, with --scan, a TCP sweep of the local subnets. Each
candidate is then checked for a DSM Web API on ports 5000/5001.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			devices, err := discover.Run(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(devices)
			}
			if len(devices) == 0 {
				fmt.Fprintln(os.Stderr, "No Synology devices found. Try --scan.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			fmt.Fprintln(w, "NAME\tIP\tMAC\tDSM\tSOURCE")
			for _, d := range devices {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					orDash(d.Name), d.IP, orDash(d.MAC), orDash(d.DSMURL), strings.Join(d.Sources, ","))
			}
			return w.Flush()
		},
	}

	c.Flags().DurationVarP(&opts.Timeout, "timeout", "t", 3*time.Second, "How long to listen for mDNS responses")
	c.Flags().BoolVar(&opts.Scan, "scan", false, "Also sweep local /24 subnets for DSM ports")
	c.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return c
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
