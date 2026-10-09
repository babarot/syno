package doctor

import (
	"context"
	"fmt"
	"time"
)

func checkPools(ctx context.Context, env *Env) (Result, error) {
	st, err := env.Source.Storage(ctx)
	if err != nil {
		return Result{}, err
	}
	var f findings
	for _, p := range st.StoragePools {
		name := poolName(p)
		switch {
		case p.DiskFailureNumber > 0:
			f.add(Fail, "%s has %s", name, plural(p.DiskFailureNumber, "failed disk"))
		case len(p.MissingDrives) > 0:
			f.add(Fail, "%s is missing %s", name, plural(len(p.MissingDrives), "drive"))
		case isSpaceOnly(p):
			// Reported by the volumes check.
		default:
			f.add(summaryLevel(p.SummaryStatus), "%s is %s", name, p.Status)
		}
	}
	return f.result(plural(len(st.StoragePools), "pool") + " healthy"), nil
}

func checkVolumes(ctx context.Context, env *Env) (Result, error) {
	st, err := env.Source.Storage(ctx)
	if err != nil {
		return Result{}, err
	}
	th := env.Thresholds
	var f findings
	var maxUsage float64
	for _, v := range st.Volumes {
		var usage float64
		if v.Size.Total > 0 {
			usage = float64(v.Size.Used) / float64(v.Size.Total) * 100
		}
		maxUsage = max(maxUsage, usage)

		level := summaryLevel(v.SummaryStatus)
		switch {
		case usage >= th.VolumeUsageFail:
			level = Fail
		case usage >= th.VolumeUsageWarn:
			level = max(level, Warn)
		}
		msg := fmt.Sprintf("%s %.0f%% used", v.VolPath, usage)
		if v.SummaryStatus != "" && v.SummaryStatus != "normal" {
			reason := v.SummaryStatus
			if v.SpaceStatus.Detail != "" {
				reason += ": " + v.SpaceStatus.Detail
			}
			msg += " (" + reason + ")"
		}
		f.add(level, "%s", msg)
	}
	return f.result(fmt.Sprintf("%s healthy, at most %.0f%% used", plural(len(st.Volumes), "volume"), maxUsage)), nil
}

func checkDisks(ctx context.Context, env *Env) (Result, error) {
	st, err := env.Source.Storage(ctx)
	if err != nil {
		return Result{}, err
	}
	var f findings
	for _, d := range st.Disks {
		switch d.Status {
		case "normal", "initialized", "not_initialized", "":
		case "crashed", "failing", "critical":
			f.add(Fail, "%s is %s", d.Name, d.Status)
		default:
			f.add(Warn, "%s is %s", d.Name, d.Status)
		}
		switch d.SmartStatus {
		case "normal", "":
		case "failing", "abnormal", "damage", "critical":
			f.add(Fail, "%s SMART is %s", d.Name, d.SmartStatus)
		default:
			f.add(Warn, "%s SMART is %s", d.Name, d.SmartStatus)
		}
		switch {
		case d.RemainLifeDanger || d.SBDaysLeftCritical:
			f.add(Fail, "%s is near the end of its life", d.Name)
		case d.BelowRemainLifeThr || d.SBDaysLeftWarning:
			f.add(Warn, "%s is below the remaining life threshold", d.Name)
		}
		if d.Unc > 0 {
			f.add(Warn, "%s has %.0f uncorrectable sectors", d.Name, float64(d.Unc))
		}
	}
	return f.result(plural(len(st.Disks), "disk") + " healthy"), nil
}

func checkDiskTemperature(ctx context.Context, env *Env) (Result, error) {
	st, err := env.Source.Storage(ctx)
	if err != nil {
		return Result{}, err
	}
	th := env.Thresholds
	var f findings
	var hottest float64
	for _, d := range st.Disks {
		t := float64(d.Temp)
		if t <= 0 {
			continue // not reported
		}
		hottest = max(hottest, t)
		switch {
		case t >= th.DiskTempFail:
			f.add(Fail, "%s is %.0f°C", d.Name, t)
		case t >= th.DiskTempWarn:
			f.add(Warn, "%s is %.0f°C", d.Name, t)
		}
	}
	return f.result(fmt.Sprintf("hottest disk is %.0f°C", hottest)), nil
}

func checkScrubbing(ctx context.Context, env *Env) (Result, error) {
	st, err := env.Source.Storage(ctx)
	if err != nil {
		return Result{}, err
	}
	var f findings
	var oldest time.Duration
	for _, p := range st.StoragePools {
		name := poolName(p)
		if p.LastDoneTime == 0 {
			f.add(Warn, "%s has never been scrubbed", name)
		} else {
			age := env.Now.Sub(time.Unix(p.LastDoneTime, 0))
			oldest = max(oldest, age)
			if age > env.Thresholds.ScrubAgeWarn {
				f.add(Warn, "%s was last scrubbed %s ago", name, days(age))
			}
		}
		if !p.IsScheduled {
			f.add(Warn, "%s has no scrubbing schedule", name)
		}
	}
	return f.result(fmt.Sprintf("scheduled, last run at most %s ago", days(oldest))), nil
}

func checkSystemTemperature(ctx context.Context, env *Env) (Result, error) {
	sys, err := env.Source.SystemInfo(ctx)
	if err != nil {
		return Result{}, err
	}
	var f findings
	if sys.SysTempWarn || sys.TemperatureWarning {
		f.add(Warn, "DSM reports high temperature (%.0f°C)", float64(sys.SysTemp))
	}
	return f.result(fmt.Sprintf("%.0f°C, no warning from DSM", float64(sys.SysTemp))), nil
}

func days(d time.Duration) string {
	return plural(int(d.Hours()/24), "day")
}
