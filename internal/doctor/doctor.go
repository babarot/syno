// Package doctor runs health checks against a DSM host.
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
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
)

func (l Level) String() string {
	switch l {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Fail:
		return "fail"
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
	Name string
	Run  func(ctx context.Context, env *Env) (Result, error)
}

// Checks lists every check in the order they are reported.
var Checks = []Check{
	{"pools", checkPools},
	{"volumes", checkVolumes},
	{"disks", checkDisks},
	{"disk-temperature", checkDiskTemperature},
	{"scrubbing", checkScrubbing},
	{"system-temperature", checkSystemTemperature},
	{"reboot", checkReboot},
	{"dsm-update", checkDSMUpdate},
	{"security-advisor", checkSecurityAdvisor},
	{"certificates", checkCertificates},
}

// Run runs every check. Data is fetched once and shared between checks.
func Run(ctx context.Context, src Source, th Thresholds, now time.Time) []Result {
	env := &Env{Source: newCachedSource(src), Thresholds: th, Now: now}
	results := make([]Result, 0, len(Checks))
	for _, c := range Checks {
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
