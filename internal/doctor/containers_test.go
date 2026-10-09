package doctor

import (
	"errors"
	"testing"

	"github.com/babarot/syno/internal/dsm"
)

func container(name, state string, running bool, health string) dsm.Container {
	var c dsm.Container
	h := ""
	if health != "" {
		h = `, "Health": {"Status": "` + health + `"}`
	}
	b := "false"
	if running {
		b = "true"
	}
	mustUnmarshal(`{"name": "`+name+`", "State": {"Status": "`+state+`", "Running": `+b+h+`}}`, &c)
	return c
}

func TestContainers(t *testing.T) {
	tests := []struct {
		name    string
		ctrs    []dsm.Container
		want    Level
		summary string
	}{
		{
			name: "healthy",
			ctrs: []dsm.Container{
				container("app", "running", true, "healthy"),
				container("proxy", "running", true, ""),
				container("job", "running", true, "starting"),
			},
			want:    OK,
			summary: "3 running, 0 stopped",
		},
		{
			name: "stopped ones are only counted",
			ctrs: []dsm.Container{
				container("app", "running", true, ""),
				container("old", "exited", false, "unhealthy"),
				container("batch", "exited", false, ""),
			},
			want:    OK,
			summary: "1 running, 2 stopped",
		},
		{
			name:    "unhealthy",
			ctrs:    []dsm.Container{container("app", "running", true, "unhealthy")},
			want:    Fail,
			summary: "app is unhealthy",
		},
		{
			name:    "restarting",
			ctrs:    []dsm.Container{container("app", "restarting", true, "")},
			want:    Fail,
			summary: "app keeps restarting",
		},
		{
			name:    "none",
			want:    OK,
			summary: "0 running, 0 stopped",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runSource(&fakeSource{ctrs: tt.ctrs})["containers"]
			if r.Level != tt.want || r.Summary != tt.summary {
				t.Errorf("got %s %q, want %s %q", r.Level, r.Summary, tt.want, tt.summary)
			}
		})
	}
}

func TestContainersWithoutContainerManager(t *testing.T) {
	src := &fakeSource{ctrsErr: &dsm.APIError{API: dsm.ContainerAPI, Method: "list", Code: 102}}
	r := runSource(src)["containers"]
	if r.Level != Skip {
		t.Errorf("got %s %q, want skip", r.Level, r.Summary)
	}
	if got := ExitCode([]Result{r}); got != 0 {
		t.Errorf("exit code = %d, want 0", got)
	}
}

func TestContainersErrorIsUnknown(t *testing.T) {
	for _, err := range []error{
		errors.New("connection refused"),
		&dsm.APIError{API: dsm.ContainerAPI, Method: "list", Code: 105},
	} {
		if r := runSource(&fakeSource{ctrsErr: err})["containers"]; r.Level != Unknown {
			t.Errorf("%v: got %s, want unknown", err, r.Level)
		}
	}
}
