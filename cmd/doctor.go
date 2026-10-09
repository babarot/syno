package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/babarot/syno/internal/doctor"
)

func newDoctorCmd() *cobra.Command {
	var host string
	var asJSON bool

	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check the health of the NAS",
		Long: `Check the health of the NAS: storage pools, volumes, disks, data scrubbing
and temperatures.

The exit status follows the Nagios plugin convention, so the command can be
used from cron or a monitoring system:

  0  every check is OK
  1  at least one check warns
  2  at least one check fails
  3  a check could not run, or the NAS could not be reached`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, err := connect(ctx, host)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}
			defer client.Logout(ctx)

			results := doctor.Run(ctx, client, doctor.DefaultThresholds(), time.Now())
			code := doctor.ExitCode(results)

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(struct {
					Host    string          `json:"host"`
					Status  string          `json:"status"`
					Results []doctor.Result `json:"results"`
				}{client.Base, exitStatus(code), results}); err != nil {
					return err
				}
			} else {
				printResults(os.Stdout, results, term.IsTerminal(int(os.Stdout.Fd())))
			}

			if code != 0 {
				return &ExitError{Code: code}
			}
			return nil
		},
	}

	c.Flags().StringVar(&host, "host", "", "DSM URL (default: the one saved by `syno login`)")
	c.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return c
}

func exitStatus(code int) string {
	return [...]string{"ok", "warn", "fail", "unknown"}[code]
}

var levelMarks = map[doctor.Level]struct {
	mark  string
	color string // ANSI color code
}{
	doctor.OK:      {"✓", "32"},
	doctor.Warn:    {"!", "33"},
	doctor.Fail:    {"✗", "31"},
	doctor.Unknown: {"?", "35"},
}

func printResults(out io.Writer, results []doctor.Result, color bool) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	counts := map[doctor.Level]int{}
	for _, r := range results {
		counts[r.Level]++
		m := levelMarks[r.Level]
		mark := m.mark
		if color {
			mark = "\x1b[" + m.color + "m" + mark + "\x1b[0m"
		}
		fmt.Fprintf(w, "%s %s\t%s\n", mark, r.Check, r.Summary)
		for _, d := range r.Details {
			fmt.Fprintf(w, "  \t- %s\n", d)
		}
	}
	w.Flush()

	var parts []string
	for _, l := range []doctor.Level{doctor.OK, doctor.Warn, doctor.Fail, doctor.Unknown} {
		if n := counts[l]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, l))
		}
	}
	fmt.Fprintf(out, "\n%s\n", strings.Join(parts, ", "))
}
