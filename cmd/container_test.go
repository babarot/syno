package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/babarot/syno/internal/dsm"
)

func testContainers(t *testing.T) []dsm.Container {
	t.Helper()
	var cs []dsm.Container
	err := json.Unmarshal([]byte(`[
		{"name": "web-app-1", "image": "example/app:latest", "status": "running", "up_status": "Up 2 days",
		 "State": {"Status": "running", "Running": true, "Health": {"Status": "healthy"}},
		 "Labels": {"com.docker.compose.project": "web"}, "Env": ["SECRET=hunter2"]},
		{"name": "web-worker-1", "image": "example/worker:latest", "status": "stopped", "up_status": "Exited (1) 3 hours ago",
		 "State": {"Status": "exited", "Running": false, "ExitCode": 1},
		 "Labels": {"com.docker.compose.project": "web"}},
		{"name": "proxy", "image": "example/proxy:1", "status": "running", "up_status": "Up 5 days",
		 "State": {"Status": "running", "Running": true}}
	]`), &cs)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func names(rows []containerRow) string {
	var s []string
	for _, r := range rows {
		s = append(s, r.Name)
	}
	return strings.Join(s, ",")
}

func TestContainerRows(t *testing.T) {
	cs := testContainers(t)
	tests := []struct {
		running bool
		project string
		want    string
	}{
		{false, "", "web-app-1,web-worker-1,proxy"},
		{true, "", "web-app-1,proxy"},
		{false, "web", "web-app-1,web-worker-1"},
		{true, "web", "web-app-1"},
		{false, "none", ""},
	}
	for _, tt := range tests {
		if got := names(containerRows(cs, tt.running, tt.project)); got != tt.want {
			t.Errorf("containerRows(running=%v, project=%q) = %q, want %q", tt.running, tt.project, got, tt.want)
		}
	}
}

func TestPrintContainers(t *testing.T) {
	var buf bytes.Buffer
	if err := printContainers(&buf, containerRows(testContainers(t), false, ""), false); err != nil {
		t.Fatal(err)
	}
	want := `NAME           STATE     HEALTH    PROJECT   IMAGE                   STATUS
web-app-1      running   healthy   web       example/app:latest      Up 2 days
web-worker-1   exited    -         web       example/worker:latest   Exited (1) 3 hours ago
proxy          running   -         -         example/proxy:1         Up 5 days
`
	if got := buf.String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestContainerRowsJSONHasNoEnv(t *testing.T) {
	b, err := json.Marshal(containerRows(testContainers(t), false, ""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "hunter2") {
		t.Errorf("JSON leaks the environment: %s", b)
	}
}

type fakeStats struct {
	samples []map[string]dsm.ContainerStat
	calls   int
}

func (f *fakeStats) ContainerStats(context.Context) (map[string]dsm.ContainerStat, error) {
	s := f.samples[f.calls]
	f.calls++
	return s, nil
}

func stat(t *testing.T, total, system, mem, inactive float64) dsm.ContainerStat {
	t.Helper()
	var s dsm.ContainerStat
	b, _ := json.Marshal(map[string]any{
		"cpu_stats":    map[string]any{"cpu_usage": map[string]any{"total_usage": total}, "system_cpu_usage": system, "online_cpus": 4},
		"memory_stats": map[string]any{"usage": mem, "stats": map[string]any{"inactive_file": inactive}},
	})
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAddUsage(t *testing.T) {
	defer func(d time.Duration) { usageInterval = d }(usageInterval)
	usageInterval = 0
	rows := []containerRow{
		{Name: "web-app-1", State: "running", Image: "example/app:latest", Status: "Up 2 days"},
		{Name: "old-job", State: "exited", Image: "example/job:latest", Status: "Exited (0) 7 months ago"},
		{Name: "idle", State: "running", Image: "example/idle:1", Status: "Up 1 hour"},
	}
	stopped := stat(t, 0, 0, 0, 0)
	f := &fakeStats{samples: []map[string]dsm.ContainerStat{
		{"web-app-1": stat(t, 1e9, 100e9, 0, 0), "old-job": stopped, "idle": stat(t, 5e9, 100e9, 0, 0)},
		// web-app-1 used 0.5 s while the system counted 10 s: 20% of one core.
		{"web-app-1": stat(t, 1.5e9, 110e9, 40<<20, 8<<20), "old-job": stopped, "idle": stat(t, 5e9, 100e9, 1<<20, 0)},
	}}
	if err := addUsage(context.Background(), f, rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].CPUPercent == nil || rows[0].MemoryBytes == nil {
		t.Fatalf("web-app-1 has no usage: %+v", rows[0])
	}
	if got := *rows[0].CPUPercent; got != 20 {
		t.Errorf("web-app-1 cpu = %v, want 20 (0.5 s of 10 s system time x 4 CPUs)", got)
	}
	if got := *rows[0].MemoryBytes; got != 32<<20 {
		t.Errorf("web-app-1 memory = %v, want usage minus inactive_file", got)
	}
	if rows[1].CPUPercent != nil || rows[1].MemoryBytes != nil {
		t.Errorf("a stopped container should have no usage: %+v", rows[1])
	}
	// The system time did not move between the samples: no CPU figure
	// rather than a division by zero.
	if rows[2].CPUPercent != nil || rows[2].MemoryBytes == nil {
		t.Errorf("idle = cpu %v, memory %v", rows[2].CPUPercent, rows[2].MemoryBytes)
	}

	var buf bytes.Buffer
	if err := printContainers(&buf, rows, true); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"NAME        STATE     HEALTH   PROJECT   IMAGE                CPU     MEM       STATUS\n" +
		"web-app-1   running   -        -         example/app:latest   20.0%   32.0 MB   Up 2 days\n" +
		"old-job     exited    -        -         example/job:latest   -       -         Exited (0) 7 months ago\n" +
		"idle        running   -        -         example/idle:1       -       1.0 MB    Up 1 hour\n"
	if got := buf.String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
