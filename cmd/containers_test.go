package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

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
	if err := printContainers(&buf, containerRows(testContainers(t), false, "")); err != nil {
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
