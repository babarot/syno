package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/babarot/syno/internal/dsm"
)

type fakeSource struct {
	sys     *dsm.SystemInfo
	st      *dsm.Storage
	reboot  bool
	upgrade *dsm.UpgradeCheck
	scan    *dsm.SecurityScan
	certs   []dsm.Certificate
	err     error
	stHits  int
}

func (f *fakeSource) SystemInfo(context.Context) (*dsm.SystemInfo, error) { return f.sys, f.err }
func (f *fakeSource) Storage(context.Context) (*dsm.Storage, error) {
	f.stHits++
	return f.st, f.err
}
func (f *fakeSource) NeedReboot(context.Context) (bool, error) { return f.reboot, f.err }
func (f *fakeSource) CheckUpgrade(context.Context) (*dsm.UpgradeCheck, error) {
	if f.upgrade == nil {
		return &dsm.UpgradeCheck{}, f.err
	}
	return f.upgrade, f.err
}
func (f *fakeSource) SecurityScan(context.Context) (*dsm.SecurityScan, error) {
	if f.scan == nil {
		return healthyScan(), f.err
	}
	return f.scan, f.err
}
func (f *fakeSource) Certificates(context.Context) ([]dsm.Certificate, error) { return f.certs, f.err }

func healthyScan() *dsm.SecurityScan {
	var s dsm.SecurityScan
	mustUnmarshal(`{
		"sysStatus": "safe",
		"lastScanTime": "`+itoa(now.Add(-3*24*time.Hour).Unix())+`",
		"items": {
			"malware": {"category": "malware", "failSeverity": "safe", "fail": {"danger": 0, "risk": 0, "warning": 0, "outOfDate": 0, "info": 0}},
			"network": {"category": "network", "failSeverity": "safe", "fail": {"danger": 0, "risk": 0, "warning": 0, "outOfDate": 0, "info": 0}}
		}
	}`, &s)
	return &s
}

func cert(desc string, validTill time.Time, renewable bool, services int) dsm.Certificate {
	c := dsm.Certificate{Desc: desc, Renewable: renewable, ValidTill: validTill.UTC().Format("Jan _2 15:04:05 2006 GMT")}
	for range services {
		c.Services = append(c.Services, json.RawMessage(`{}`))
	}
	return c
}

var now = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)

func healthyStorage() *dsm.Storage {
	var st dsm.Storage
	mustUnmarshal(`{
		"storagePools": [{
			"id": "reuse_1", "num_id": 1, "status": "normal", "summary_status": "normal",
			"space_status": {"status": "pool_normal"},
			"last_done_time": `+itoa(now.Add(-10*24*time.Hour).Unix())+`, "is_scheduled": true
		}],
		"volumes": [{
			"vol_path": "/volume1", "status": "normal", "summary_status": "normal",
			"size": {"total": "1000", "used": "500"}
		}],
		"disks": [
			{"name": "Drive 1", "status": "normal", "smart_status": "normal", "temp": 35},
			{"name": "Drive 2", "status": "normal", "smart_status": "normal", "temp": 36}
		]
	}`, &st)
	return &st
}

func mustUnmarshal(s string, v any) {
	if err := json.Unmarshal([]byte(s), v); err != nil {
		panic(err)
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func run(st *dsm.Storage, sys *dsm.SystemInfo) map[string]Result {
	if sys == nil {
		sys = &dsm.SystemInfo{SysTemp: 45}
	}
	return runSource(&fakeSource{sys: sys, st: st})
}

func runSource(src *fakeSource) map[string]Result {
	if src.sys == nil {
		src.sys = &dsm.SystemInfo{SysTemp: 45}
	}
	if src.st == nil {
		src.st = healthyStorage()
	}
	out := map[string]Result{}
	for _, r := range Run(context.Background(), src, Options{Thresholds: DefaultThresholds(), Now: now}) {
		out[r.Check] = r
	}
	return out
}

func TestHealthy(t *testing.T) {
	results := run(healthyStorage(), nil)
	for name, r := range results {
		if r.Level != OK {
			t.Errorf("%s: got %s (%s), want ok", name, r.Level, r.Summary)
		}
	}
	if got := results["volumes"].Summary; got != "1 volume healthy, at most 50% used" {
		t.Errorf("volumes summary = %q", got)
	}
}

func TestVolumeUsage(t *testing.T) {
	tests := []struct {
		used string
		want Level
	}{
		{"840", OK},
		{"850", Warn},
		{"950", Fail},
	}
	for _, tt := range tests {
		st := healthyStorage()
		mustUnmarshal(`{"total": "1000", "used": "`+tt.used+`"}`, &st.Volumes[0].Size)
		if got := run(st, nil)["volumes"].Level; got != tt.want {
			t.Errorf("used %s/1000: got %s, want %s", tt.used, got, tt.want)
		}
	}
}

func TestSpaceOnlyPoolIsLeftToVolumes(t *testing.T) {
	st := healthyStorage()
	st.StoragePools[0].Status = "attention"
	st.StoragePools[0].SummaryStatus = "attention"
	st.StoragePools[0].SpaceStatus.Detail = "fs_almost_full"
	st.Volumes[0].SummaryStatus = "attention"
	st.Volumes[0].SpaceStatus.Detail = "fs_almost_full"

	results := run(st, nil)
	if got := results["pools"].Level; got != OK {
		t.Errorf("pools: got %s, want ok", got)
	}
	r := results["volumes"]
	if r.Level != Warn || r.Summary != "/volume1 50% used (attention: fs_almost_full)" {
		t.Errorf("volumes: got %s %q", r.Level, r.Summary)
	}
}

func TestPoolFailures(t *testing.T) {
	st := healthyStorage()
	st.StoragePools[0].DiskFailureNumber = 1
	if r := run(st, nil)["pools"]; r.Level != Fail || r.Summary != "Pool 1 has 1 failed disk" {
		t.Errorf("got %s %q", r.Level, r.Summary)
	}

	st = healthyStorage()
	st.StoragePools[0].Status = "degraded"
	st.StoragePools[0].SummaryStatus = "danger"
	st.StoragePools[0].SpaceStatus.Status = "pool_degraded"
	if r := run(st, nil)["pools"]; r.Level != Fail || r.Summary != "Pool 1 is degraded" {
		t.Errorf("got %s %q", r.Level, r.Summary)
	}
}

func TestDisks(t *testing.T) {
	st := healthyStorage()
	st.Disks[0].SmartStatus = "failing"
	st.Disks[1].Unc = 3
	r := run(st, nil)["disks"]
	if r.Level != Fail || len(r.Details) != 2 {
		t.Errorf("got %s %q %v", r.Level, r.Summary, r.Details)
	}
}

func TestDiskTemperature(t *testing.T) {
	st := healthyStorage()
	st.Disks[1].Temp = 55
	if r := run(st, nil)["disk-temperature"]; r.Level != Warn || r.Summary != "Drive 2 is 55°C" {
		t.Errorf("got %s %q", r.Level, r.Summary)
	}
}

func TestScrubbing(t *testing.T) {
	st := healthyStorage()
	st.StoragePools[0].LastDoneTime = now.Add(-200 * 24 * time.Hour).Unix()
	st.StoragePools[0].IsScheduled = false
	r := run(st, nil)["scrubbing"]
	want := []string{"Pool 1 was last scrubbed 200 days ago", "Pool 1 has no scrubbing schedule"}
	if r.Level != Warn || len(r.Details) != 2 || r.Details[0] != want[0] || r.Details[1] != want[1] {
		t.Errorf("got %s %q %v", r.Level, r.Summary, r.Details)
	}

	st = healthyStorage()
	st.StoragePools[0].LastDoneTime = 0
	if r := run(st, nil)["scrubbing"]; r.Level != Warn || r.Summary != "Pool 1 has never been scrubbed" {
		t.Errorf("got %s %q", r.Level, r.Summary)
	}
}

func TestSystemTemperature(t *testing.T) {
	r := run(healthyStorage(), &dsm.SystemInfo{SysTemp: 80, SysTempWarn: true})["system-temperature"]
	if r.Level != Warn {
		t.Errorf("got %s %q", r.Level, r.Summary)
	}
}

func TestAPIErrorIsUnknown(t *testing.T) {
	src := &fakeSource{err: errors.New("permission denied")}
	results := Run(context.Background(), src, Options{Thresholds: DefaultThresholds(), Now: now})
	for _, r := range results {
		if r.Level != Unknown {
			t.Errorf("%s: got %s, want unknown", r.Check, r.Level)
		}
	}
	if got := ExitCode(results); got != 3 {
		t.Errorf("exit code = %d, want 3", got)
	}
}

func TestStorageIsFetchedOnce(t *testing.T) {
	src := &fakeSource{sys: &dsm.SystemInfo{}, st: healthyStorage()}
	Run(context.Background(), src, Options{Thresholds: DefaultThresholds(), Now: now})
	if src.stHits != 1 {
		t.Errorf("Storage called %d times, want 1", src.stHits)
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		levels []Level
		want   int
	}{
		{[]Level{OK, OK}, 0},
		{[]Level{OK, Warn}, 1},
		{[]Level{Warn, Fail}, 2},
		{[]Level{Warn, Unknown}, 3},
		{[]Level{Unknown, Fail}, 2},
	}
	for _, tt := range tests {
		var results []Result
		for _, l := range tt.levels {
			results = append(results, Result{Level: l})
		}
		if got := ExitCode(results); got != tt.want {
			t.Errorf("%v: got %d, want %d", tt.levels, got, tt.want)
		}
	}
}

func TestReboot(t *testing.T) {
	if r := runSource(&fakeSource{reboot: true})["reboot"]; r.Level != Warn {
		t.Errorf("got %s %q", r.Level, r.Summary)
	}
}

func TestDSMUpdate(t *testing.T) {
	var up dsm.UpgradeCheck
	mustUnmarshal(`{"update": {"available": true, "version": "DSM 7.9.9-99999", "version_details": {"isSecurityVersion": true}}}`, &up)
	r := runSource(&fakeSource{upgrade: &up})["dsm-update"]
	if r.Level != Warn || r.Summary != "DSM 7.9.9-99999 is available (security update)" {
		t.Errorf("got %s %q", r.Level, r.Summary)
	}
}

func TestSecurityAdvisor(t *testing.T) {
	if r := runSource(&fakeSource{})["security-advisor"]; r.Level != OK || r.Summary != "no findings, last scan 3 days ago" {
		t.Errorf("healthy: got %s %q", r.Level, r.Summary)
	}

	scan := healthyScan()
	item := scan.Items["network"]
	item.FailSeverity = "risk"
	item.Fail = map[string]int{"risk": 2, "warning": 1}
	scan.Items["network"] = item
	if r := runSource(&fakeSource{scan: scan})["security-advisor"]; r.Level != Fail || r.Summary != "network: 2 risk, 1 warning" {
		t.Errorf("risk: got %s %q", r.Level, r.Summary)
	}

	scan = healthyScan()
	scan.LastScanTime = dsm.Num(now.Add(-40 * 24 * time.Hour).Unix())
	if r := runSource(&fakeSource{scan: scan})["security-advisor"]; r.Level != Warn || r.Summary != "last scan was 40 days ago" {
		t.Errorf("old scan: got %s %q", r.Level, r.Summary)
	}
}

func TestCertificates(t *testing.T) {
	day := 24 * time.Hour
	tests := []struct {
		name string
		cert dsm.Certificate
		want Level
		sum  string
	}{
		{"valid", cert("web", now.Add(60*day), false, 1), OK, "1 certificate valid, next expiry in 60 days"},
		{"expired and used", cert("web", now.Add(-5*day), false, 2), Fail, `certificate "web" expired 5 days ago and is used by 2 services`},
		{"expired and unused", cert("old", now.Add(-5*day), false, 0), Warn, `certificate "old" expired 5 days ago (not used by any service)`},
		{"manual expiring", cert("web", now.Add(20*day), false, 1), Warn, `certificate "web" expires in 20 days`},
		{"renewable expiring later", cert("le", now.Add(20*day), true, 1), OK, "1 certificate valid, next expiry in 20 days"},
		{"renewable expiring soon", cert("le", now.Add(10*day), true, 1), Warn, `certificate "le" expires in 10 days, automatic renewal may be failing`},
	}
	for _, tt := range tests {
		r := runSource(&fakeSource{certs: []dsm.Certificate{tt.cert}})["certificates"]
		if r.Level != tt.want || r.Summary != tt.sum {
			t.Errorf("%s: got %s %q, want %s %q", tt.name, r.Level, r.Summary, tt.want, tt.sum)
		}
	}
}

func TestSkip(t *testing.T) {
	st := healthyStorage()
	st.Volumes[0].SummaryStatus = "danger"
	src := &fakeSource{sys: &dsm.SystemInfo{}, st: st}
	results := Run(context.Background(), src, Options{Thresholds: DefaultThresholds(), Now: now, Skip: []string{"volumes"}})

	byName := map[string]Result{}
	for _, r := range results {
		byName[r.Check] = r
	}
	if r := byName["volumes"]; r.Level != Skip || r.Summary != "skipped" {
		t.Errorf("volumes: got %s %q, want skip", r.Level, r.Summary)
	}
	if len(results) != len(Checks) {
		t.Errorf("got %d results, want every check listed", len(results))
	}
	if got := ExitCode(results); got != 0 {
		t.Errorf("exit code = %d, want 0 with the failing check skipped", got)
	}
}

// A missing API is unusual for the core APIs the checks use, so it must
// not be hidden as a skip.
func TestMissingAPIIsUnknown(t *testing.T) {
	src := &fakeSource{err: &dsm.APIError{API: "SYNO.Storage.CGI.Storage", Method: "load_info", Code: 102}}
	for _, r := range Run(context.Background(), src, Options{Thresholds: DefaultThresholds(), Now: now}) {
		if r.Level != Unknown {
			t.Errorf("%s: got %s, want unknown", r.Check, r.Level)
		}
	}
}
