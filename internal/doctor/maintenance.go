package doctor

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/babarot/syno/internal/dsm"
)

func checkReboot(ctx context.Context, env *Env) (Result, error) {
	need, err := env.Source.NeedReboot(ctx)
	if err != nil {
		return Result{}, err
	}
	var f findings
	if need {
		f.add(Warn, "a reboot is pending to finish an update")
	}
	return f.result("no reboot pending"), nil
}

func checkDSMUpdate(ctx context.Context, env *Env) (Result, error) {
	up, err := env.Source.CheckUpgrade(ctx)
	if err != nil {
		return Result{}, err
	}
	var f findings
	if u := up.Update; u.Available {
		if u.VersionDetails.IsSecurityVersion {
			f.add(Warn, "%s is available (security update)", u.Version)
		} else {
			f.add(Warn, "%s is available", u.Version)
		}
	}
	return f.result("DSM is up to date"), nil
}

// securityLevels maps the Security Advisor's severities to a Level.
// "safe" and "info" are OK.
var securityLevels = map[string]Level{
	"warning":   Warn,
	"outOfDate": Warn,
	"risk":      Fail,
	"danger":    Fail,
}

func checkSecurityAdvisor(ctx context.Context, env *Env) (Result, error) {
	scan, err := env.Source.SecurityScan(ctx)
	if err != nil {
		return Result{}, err
	}
	var f findings

	categories := make([]string, 0, len(scan.Items))
	for name := range scan.Items {
		categories = append(categories, name)
	}
	sort.Strings(categories)
	for _, name := range categories {
		item := scan.Items[name]
		var counts []string
		level := OK
		for _, sev := range []string{"danger", "risk", "warning", "outOfDate"} {
			if n := item.Fail[sev]; n > 0 {
				counts = append(counts, fmt.Sprintf("%d %s", n, sev))
				level = max(level, securityLevels[sev])
			}
		}
		level = max(level, securityLevels[item.FailSeverity])
		if level != OK {
			f.add(level, "%s: %s", name, strings.Join(counts, ", "))
		}
	}

	last := int64(scan.LastScanTime)
	if last == 0 {
		f.add(Warn, "Security Advisor has never run")
		return f.result(""), nil
	}
	age := env.Now.Sub(time.Unix(last, 0))
	if age > env.Thresholds.SecurityScanAgeWarn {
		f.add(Warn, "last scan was %s ago", days(age))
	}
	return f.result(fmt.Sprintf("no findings, last scan %s ago", days(age))), nil
}

func checkCertificates(ctx context.Context, env *Env) (Result, error) {
	certs, err := env.Source.Certificates(ctx)
	if err != nil {
		return Result{}, err
	}
	th := env.Thresholds
	var f findings
	var soonest time.Duration = -1
	for _, c := range certs {
		name := certName(c)
		if c.IsBroken {
			f.add(Fail, "%s is broken", name)
			continue
		}
		expiry, err := c.Expiry()
		if err != nil {
			f.add(Warn, "%s has an unreadable expiry %q", name, c.ValidTill)
			continue
		}
		left := expiry.Sub(env.Now)
		used := len(c.Services) > 0
		switch {
		case left <= 0 && used:
			f.add(Fail, "%s expired %s ago and is used by %s", name, days(-left), plural(len(c.Services), "service"))
		case left <= 0:
			f.add(Warn, "%s expired %s ago (not used by any service)", name, days(-left))
		case c.Renewable && left < th.RenewableCertExpiryWarn:
			f.add(Warn, "%s expires in %s, automatic renewal may be failing", name, days(left))
		case !c.Renewable && left < th.CertExpiryWarn:
			f.add(Warn, "%s expires in %s", name, days(left))
		}
		if left > 0 && (soonest < 0 || left < soonest) {
			soonest = left
		}
	}
	if len(certs) == 0 {
		return f.result("no certificates"), nil
	}
	return f.result(fmt.Sprintf("%s valid, next expiry in %s", plural(len(certs), "certificate"), days(soonest))), nil
}

// certName names a certificate the way DSM lists it: its description, or its
// common name when there is none.
func certName(c dsm.Certificate) string {
	name := c.Desc
	if name == "" {
		name = c.Subject.CommonName
	}
	if name == "" {
		name = c.ID
	}
	if c.IsDefault {
		name += " (default)"
	}
	return fmt.Sprintf("certificate %q", name)
}
