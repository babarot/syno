package cmd

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"text/tabwriter"

	"github.com/babarot/syno/internal/dsm"
)

func TestFormatUptime(t *testing.T) {
	tests := map[string]string{
		"1234:5:6": "51d 10h 5m",
		"0:42:00":  "0d 0h 42m",
		"bogus":    "bogus",
	}
	for in, want := range tests {
		if got := formatUptime(in); got != want {
			t.Errorf("formatUptime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := map[float64]string{
		512:           "512 B",
		1536:          "1.5 KB",
		3999969443840: "3.6 TB",
	}
	for in, want := range tests {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatHours(t *testing.T) {
	tests := map[float64]string{
		5:     "5h",
		47:    "47h",
		48:    "2d",
		960:   "40d",
		23219: "2.7y",
	}
	for in, want := range tests {
		if got := formatHours(in); got != want {
			t.Errorf("formatHours(%v) = %q, want %q", in, got, want)
		}
	}
}

type fakeDiskHealth map[string]float64

func (f fakeDiskHealth) DiskHealth(_ context.Context, device string) (*dsm.DiskHealth, error) {
	h, ok := f[device]
	if !ok {
		return nil, &dsm.APIError{API: "SYNO.Storage.CGI.Smart", Method: "get_health_info", Code: 117}
	}
	return &dsm.DiskHealth{PowerOnHours: dsm.Num(h)}, nil
}

func TestPowerOnHours(t *testing.T) {
	disks := []dsm.Disk{
		{ID: "sata1", Device: "/dev/sata1"},
		{ID: "sata2", Device: "/dev/sata2"}, // the SMART overview fails
		{ID: "sata3"},                       // no device to ask for
		{ID: "sata4", Device: "/dev/sata4"}, // DSM answers 0
	}
	got := powerOnHours(context.Background(), fakeDiskHealth{"/dev/sata1": 23219, "/dev/sata4": 0}, disks)
	if len(got) != 1 || got["sata1"] != 23219 {
		t.Errorf("got %v, want only sata1", got)
	}
}

func TestStatusReportDisks(t *testing.T) {
	var st dsm.Storage
	if err := json.Unmarshal([]byte(`{"disks":[
		{"id":"sata1","name":"Drive 1","remain_life":{"value":-1}},
		{"id":"nvme0n1","name":"M.2 Drive 1","remain_life":{"value":87}}
	]}`), &st); err != nil {
		t.Fatal(err)
	}
	s := &status{sys: &dsm.SystemInfo{}, util: &dsm.Utilization{}, st: &st, powerOn: map[string]float64{"sata1": 23219}}
	r := s.report()

	hdd, ssd := r.Disks[0], r.Disks[1]
	if hdd.LifePercent != nil || hdd.PowerOnHours == nil || *hdd.PowerOnHours != 23219 {
		t.Errorf("Drive 1 = life %v, hours %v", hdd.LifePercent, hdd.PowerOnHours)
	}
	if ssd.LifePercent == nil || *ssd.LifePercent != 87 || ssd.PowerOnHours != nil {
		t.Errorf("M.2 Drive 1 = life %v, hours %v", ssd.LifePercent, ssd.PowerOnHours)
	}

	b, err := json.Marshal(hdd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "life_percent") {
		t.Errorf("a disk without an estimate has life_percent: %s", b)
	}

	var out strings.Builder
	w := tabwriter.NewWriter(&out, 0, 0, 3, ' ', 0)
	printDisks(w, &st, s.powerOn)
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if !strings.Contains(lines[0], "LIFE") || !strings.Contains(lines[0], "POWER-ON") {
		t.Errorf("header = %q", lines[0])
	}
	if f := strings.Fields(lines[1]); !slices.Contains(f, "2.7y") {
		t.Errorf("Drive 1 row = %q", lines[1])
	}
	if f := strings.Fields(lines[2]); !slices.Contains(f, "87%") {
		t.Errorf("M.2 row = %q", lines[2])
	}
}
