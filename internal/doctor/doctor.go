// Package doctor runs health checks against a DSM host.
package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/babarot/syno/internal/dsm"
)

// Level is the outcome of a check, ordered by severity.
type Level int

const (
	OK Level = iota
	Warn
	Fail
	// Unknown means the check could not run, for example because an API
	// call failed.
	Unknown
	// Skip means the check was turned off. Like OK, it does not change the
	// exit status.
	Skip
)

func (l Level) String() string {
	switch l {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Fail:
		return "fail"
	case Skip:
		return "skip"
	default:
		return "unknown"
	}
}

func (l Level) MarshalJSON() ([]byte, error) { return json.Marshal(l.String()) }

// Result is what one check reports.
type Result struct {
	Check   string   `json:"check"`
	Level   Level    `json:"level"`
	Summary string   `json:"summary"`
	Details []string `json:"details,omitempty"`
}

// Thresholds tune the checks that compare numbers.
type Thresholds struct {
	VolumeUsageWarn float64 // percent
	VolumeUsageFail float64 // percent
	DiskTempWarn    float64 // Celsius
	DiskTempFail    float64 // Celsius
	ScrubAgeWarn    time.Duration
	// SecurityScanAgeWarn is how old the last Security Advisor scan may be.
	SecurityScanAgeWarn time.Duration
	// CertExpiryWarn is how early to warn about a certificate DSM does not
	// renew by itself.
	CertExpiryWarn time.Duration
	// RenewableCertExpiryWarn is the same for certificates DSM renews.
	// Let's Encrypt certificates are renewed about 30 days before expiry,
	// so one still close to expiry means the renewal is failing.
	RenewableCertExpiryWarn time.Duration
}

// Validate reports thresholds that cannot work, such as a warn level above
// the fail level.
func (t Thresholds) Validate() error {
	var errs []error
	if !(0 < t.VolumeUsageWarn && t.VolumeUsageWarn < t.VolumeUsageFail && t.VolumeUsageFail <= 100) {
		errs = append(errs, fmt.Errorf("volume_usage: want 0 < warn (%g) < fail (%g) <= 100", t.VolumeUsageWarn, t.VolumeUsageFail))
	}
	if !(0 < t.DiskTempWarn && t.DiskTempWarn < t.DiskTempFail) {
		errs = append(errs, fmt.Errorf("disk_temperature: want 0 < warn (%g) < fail (%g)", t.DiskTempWarn, t.DiskTempFail))
	}
	for _, d := range []struct {
		name string
		v    time.Duration
	}{
		{"scrub_age_days", t.ScrubAgeWarn},
		{"security_scan_age_days", t.SecurityScanAgeWarn},
		{"cert_expiry_days", t.CertExpiryWarn},
		{"renewable_cert_expiry_days", t.RenewableCertExpiryWarn},
	} {
		if d.v <= 0 {
			errs = append(errs, fmt.Errorf("%s: want a positive number of days", d.name))
		}
	}
	return errors.Join(errs...)
}

func DefaultThresholds() Thresholds {
	return Thresholds{
		VolumeUsageWarn: 85,
		VolumeUsageFail: 95,
		DiskTempWarn:    50,
		DiskTempFail:    60,
		ScrubAgeWarn:    90 * 24 * time.Hour,

		SecurityScanAgeWarn:     30 * 24 * time.Hour,
		CertExpiryWarn:          30 * 24 * time.Hour,
		RenewableCertExpiryWarn: 14 * 24 * time.Hour,
	}
}

// Source provides the data checks look at.
type Source interface {
	SystemInfo(ctx context.Context) (*dsm.SystemInfo, error)
	Storage(ctx context.Context) (*dsm.Storage, error)
	NeedReboot(ctx context.Context) (bool, error)
	CheckUpgrade(ctx context.Context) (*dsm.UpgradeCheck, error)
	SecurityScan(ctx context.Context) (*dsm.SecurityScan, error)
	Certificates(ctx context.Context) ([]dsm.Certificate, error)
}

// Env is passed to every check.
type Env struct {
	Source     Source
	Thresholds Thresholds
	Now        time.Time
}

// Check is one health check.
type Check struct {
	Name        string
	Description string
	Run         func(ctx context.Context, env *Env) (Result, error)
}

// Checks lists every check in the order they are reported.
var Checks = []Check{
	{"pools", "Storage pools have no failed or missing disks and DSM reports them normal", checkPools},
	{"volumes", "Volumes are below the usage thresholds and DSM reports them normal", checkVolumes},
	{"disks", "Disk status, SMART and the remaining life DSM estimates are normal", checkDisks},
	{"disk-temperature", "Disks are below the temperature thresholds", checkDiskTemperature},
	{"scrubbing", "Data scrubbing runs on a schedule and ran recently", checkScrubbing},
	{"system-temperature", "DSM does not warn about the system temperature", checkSystemTemperature},
	{"reboot", "No reboot is pending to finish an update", checkReboot},
	{"dsm-update", "DSM is up to date (the NAS asks Synology's update server)", checkDSMUpdate},
	{"security-advisor", "Security Advisor has no findings and scanned recently", checkSecurityAdvisor},
	{"certificates", "Certificates are valid and not about to expire", checkCertificates},
}

// CheckNames returns the names of every check, in report order.
func CheckNames() []string {
	names := make([]string, len(Checks))
	for i, c := range Checks {
		names[i] = c.Name
	}
	return names
}

// ValidateNames fails on names that are not checks.
func ValidateNames(names []string) error {
	valid := CheckNames()
	var unknown []string
	for _, n := range names {
		if !slices.Contains(valid, n) {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown check %s, want one of: %s", strings.Join(unknown, ", "), strings.Join(valid, ", "))
	}
	return nil
}

// Options controls a run.
type Options struct {
	Thresholds Thresholds
	Now        time.Time
	// Skip names checks to report as skipped without running them.
	Skip []string
	// Only, when not empty, names the checks to run. The others are left
	// out of the results entirely.
	Only []string
}

// Run runs the checks. Data is fetched once and shared between checks.
func Run(ctx context.Context, src Source, opts Options) []Result {
	env := &Env{Source: newCachedSource(src), Thresholds: opts.Thresholds, Now: opts.Now}
	results := make([]Result, 0, len(Checks))
	for _, c := range Checks {
		if len(opts.Only) > 0 && !slices.Contains(opts.Only, c.Name) {
			continue
		}
		if slices.Contains(opts.Skip, c.Name) {
			results = append(results, Result{Check: c.Name, Level: Skip, Summary: "skipped"})
			continue
		}
		r, err := c.Run(ctx, env)
		if err != nil {
			r = Result{Level: Unknown, Summary: err.Error()}
		}
		r.Check = c.Name
		results = append(results, r)
	}
	return results
}

// ExitCode follows the Nagios plugin convention: 0 OK, 1 WARN, 2 FAIL,
// 3 UNKNOWN. A failure wins over an unknown, which wins over a warning.
func ExitCode(results []Result) int {
	var warn, fail, unknown bool
	for _, r := range results {
		switch r.Level {
		case Warn:
			warn = true
		case Fail:
			fail = true
		case Unknown:
			unknown = true
		}
	}
	switch {
	case fail:
		return 2
	case unknown:
		return 3
	case warn:
		return 1
	default:
		return 0
	}
}

// findings collects the issues a check finds across several items.
type findings struct {
	level  Level
	issues []string
}

func (f *findings) add(l Level, format string, args ...any) {
	if l == OK {
		return
	}
	if l > f.level {
		f.level = l
	}
	f.issues = append(f.issues, fmt.Sprintf(format, args...))
}

// result reports a single issue inline and several as details.
func (f *findings) result(okSummary string) Result {
	switch len(f.issues) {
	case 0:
		return Result{Level: OK, Summary: okSummary}
	case 1:
		return Result{Level: f.level, Summary: f.issues[0]}
	default:
		return Result{Level: f.level, Summary: fmt.Sprintf("%d issues", len(f.issues)), Details: f.issues}
	}
}

// summaryLevel maps DSM's summary_status to a Level.
func summaryLevel(s string) Level {
	switch s {
	case dsm.SummaryNormal, "":
		return OK
	case dsm.SummaryDanger:
		return Fail
	default:
		return Warn
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func poolName(p dsm.StoragePool) string {
	if p.NumID > 0 {
		return fmt.Sprintf("Pool %d", p.NumID)
	}
	return p.ID
}

// isSpaceOnly reports whether a pool is flagged only because a volume on it
// is running out of space. The volumes check reports that case instead.
func isSpaceOnly(p dsm.StoragePool) bool {
	return p.SpaceStatus.Status == "pool_normal" && strings.HasPrefix(p.SpaceStatus.Detail, "fs_")
}

// cachedSource caches the APIs several checks share. The others pass
// through to the embedded Source.
type cachedSource struct {
	Source

	sysOnce sync.Once
	sys     *dsm.SystemInfo
	sysErr  error

	stOnce sync.Once
	st     *dsm.Storage
	stErr  error
}

func newCachedSource(src Source) *cachedSource { return &cachedSource{Source: src} }

func (c *cachedSource) SystemInfo(ctx context.Context) (*dsm.SystemInfo, error) {
	c.sysOnce.Do(func() { c.sys, c.sysErr = c.Source.SystemInfo(ctx) })
	return c.sys, c.sysErr
}

func (c *cachedSource) Storage(ctx context.Context) (*dsm.Storage, error) {
	c.stOnce.Do(func() { c.st, c.stErr = c.Source.Storage(ctx) })
	return c.st, c.stErr
}
