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
	sys    *dsm.SystemInfo
	st     *dsm.Storage
	err    error
	stHits int
}

func (f *fakeSource) SystemInfo(context.Context) (*dsm.SystemInfo, error) { return f.sys, f.err }
func (f *fakeSource) Storage(context.Context) (*dsm.Storage, error) {
	f.stHits++
	return f.st, f.err
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
	out := map[string]Result{}
	for _, r := range Run(context.Background(), &fakeSource{sys: sys, st: st}, DefaultThresholds(), now) {
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
	results := Run(context.Background(), src, DefaultThresholds(), now)
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
	Run(context.Background(), src, DefaultThresholds(), now)
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
