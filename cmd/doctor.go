package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/babarot/syno/internal/config"
	"github.com/babarot/syno/internal/doctor"
	"github.com/babarot/syno/internal/dsm"
)

func newDoctorCmd() *cobra.Command {
	var (
		asJSON bool
		list   bool
		skip   []string
		only   []string
	)

	c := &cobra.Command{
		Use:   "doctor",
		Short: "Check the health of the NAS",
		Long: `Check the health of the NAS: storage pools, volumes, disks, data scrubbing,
temperatures, updates, the Security Advisor, certificates and containers.
--list shows every check.

Checks can be turned off with --skip or in ~/.config/syno/config.yaml, and
the thresholds can be changed there too:

  doctor:
    skip: [dsm-update]
    thresholds:
      volume_usage: {warn: 85, fail: 95}       # percent
      disk_temperature: {warn: 50, fail: 60}   # Celsius
      scrub_age_days: 90
      security_scan_age_days: 30
      cert_expiry_days: 30
      renewable_cert_expiry_days: 14

The exit status follows the Nagios plugin convention, so the command can be
used from cron or a monitoring system:

  0  every check is OK or skipped
  1  at least one check warns
  2  at least one check fails
  3  a check could not run, the NAS could not be reached, or the settings
     are invalid`,
		Example: `  syno doctor
  syno doctor --skip dsm-update,certificates
  syno doctor --only volumes,disks --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if list {
				return printChecks(os.Stdout)
			}

			report, err := runDoctor(cmd.Context(), skip, only)
			if err != nil {
				return &ExitError{Code: 3, Err: err}
			}
			code := doctor.ExitCode(report.Results)

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(report); err != nil {
					return err
				}
			} else {
				printResults(os.Stdout, report.Results, term.IsTerminal(int(os.Stdout.Fd())))
			}

			if code != 0 {
				return &ExitError{Code: code}
			}
			return nil
		},
	}

	c.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	c.Flags().BoolVar(&list, "list", false, "List the checks and exit")
	c.Flags().StringSliceVar(&skip, "skip", nil, "Checks to turn off, added to the ones in config.yaml")
	c.Flags().StringSliceVar(&only, "only", nil, "Run only these checks, ignoring skip in config.yaml")
	c.MarkFlagsMutuallyExclusive("skip", "only")

	return c
}

// doctorReport is the JSON of syno doctor --json and the syno_doctor MCP
// tool.
type doctorReport struct {
	Host    string          `json:"host"`
	Status  string          `json:"status"`
	Results []doctor.Result `json:"results"`
}

// runDoctor runs the checks with config.yaml and the given --skip or --only.
func runDoctor(ctx context.Context, skip, only []string) (*doctorReport, error) {
	opts, err := doctorOptions(skip, only)
	if err != nil {
		return nil, err
	}
	client, release, err := connect(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return readDoctor(ctx, client, opts), nil
}

// readDoctor runs the checks with a client that is already logged in.
func readDoctor(ctx context.Context, client *dsm.Client, opts doctor.Options) *doctorReport {
	results := doctor.Run(ctx, client, opts)
	return &doctorReport{Host: client.Base, Status: exitStatus(doctor.ExitCode(results)), Results: results}
}

// doctorOptions combines config.yaml and the flags into doctor.Options.
func doctorOptions(skip, only []string) (doctor.Options, error) {
	if len(skip) > 0 && len(only) > 0 {
		return doctor.Options{}, errors.New("skip and only cannot be used together")
	}
	settings, err := config.LoadSettings()
	if err != nil {
		return doctor.Options{}, err
	}
	ds := settings.Doctor

	if err := doctor.ValidateNames(ds.Skip); err != nil {
		return doctor.Options{}, fmt.Errorf("config.yaml: doctor.skip: %w", err)
	}
	if err := doctor.ValidateNames(skip); err != nil {
		return doctor.Options{}, fmt.Errorf("--skip: %w", err)
	}
	if err := doctor.ValidateNames(only); err != nil {
		return doctor.Options{}, fmt.Errorf("--only: %w", err)
	}

	th := applyThresholds(doctor.DefaultThresholds(), ds.Thresholds)
	if err := th.Validate(); err != nil {
		return doctor.Options{}, fmt.Errorf("config.yaml: doctor.thresholds: %w", err)
	}

	opts := doctor.Options{Thresholds: th, Now: time.Now(), Only: only}
	if len(only) == 0 {
		opts.Skip = append(slices.Clone(ds.Skip), skip...)
	}
	return opts, nil
}

// applyThresholds overrides th with the values set in config.yaml.
func applyThresholds(th doctor.Thresholds, c config.DoctorThresholds) doctor.Thresholds {
	setFloat := func(dst *float64, v *float64) {
		if v != nil {
			*dst = *v
		}
	}
	setDays := func(dst *time.Duration, v *int) {
		if v != nil {
			*dst = time.Duration(*v) * 24 * time.Hour
		}
	}
	setFloat(&th.VolumeUsageWarn, c.VolumeUsage.Warn)
	setFloat(&th.VolumeUsageFail, c.VolumeUsage.Fail)
	setFloat(&th.DiskTempWarn, c.DiskTemperature.Warn)
	setFloat(&th.DiskTempFail, c.DiskTemperature.Fail)
	setDays(&th.ScrubAgeWarn, c.ScrubAgeDays)
	setDays(&th.SecurityScanAgeWarn, c.SecurityScanAgeDays)
	setDays(&th.CertExpiryWarn, c.CertExpiryDays)
	setDays(&th.RenewableCertExpiryWarn, c.RenewableCertExpiryDays)
	return th
}

func printChecks(out io.Writer) error {
	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "CHECK\tDESCRIPTION")
	for _, c := range doctor.Checks {
		fmt.Fprintf(w, "%s\t%s\n", c.Name, c.Description)
	}
	return w.Flush()
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
	doctor.Skip:    {"-", "90"},
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
	for _, l := range []doctor.Level{doctor.OK, doctor.Warn, doctor.Fail, doctor.Unknown, doctor.Skip} {
		if n := counts[l]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, l))
		}
	}
	fmt.Fprintf(out, "\n%s\n", strings.Join(parts, ", "))
}
