package cmd

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/babarot/syno/internal/dsm"
)

type fakeOps struct {
	states map[string]string // name -> state
	broken map[string]string // name -> the reason it cannot start
	acted  []string
}

func (f *fakeOps) Containers(context.Context) ([]dsm.Container, error) {
	var cs []dsm.Container
	for _, name := range []string{"web-app-1", "worker-1", "old-job"} {
		var c dsm.Container
		c.Name = name
		c.State.Status = f.states[name]
		c.State.Error = f.broken[name]
		cs = append(cs, c)
	}
	return cs, nil
}

func (f *fakeOps) ContainerAction(_ context.Context, action, name string) (*dsm.ContainerActionResult, error) {
	f.acted = append(f.acted, action+" "+name)
	if f.broken[name] != "" && action != "stop" {
		return nil, &dsm.APIError{API: dsm.ContainerAPI, Method: action, Code: 1301}
	}
	if action == "stop" {
		f.states[name] = "exited"
	} else {
		f.states[name] = "running"
	}
	return &dsm.ContainerActionResult{Name: name}, nil
}

func newFakeOps() *fakeOps {
	return &fakeOps{
		states: map[string]string{"web-app-1": "running", "worker-1": "running", "old-job": "exited"},
		broken: map[string]string{"old-job": "network 3d0e not found"},
	}
}

func runAction(t *testing.T, f *fakeOps, action string, names []string, p actionPrompt) ([]containerActionResult, string, error) {
	t.Helper()
	var out bytes.Buffer
	p.out = &out
	if p.in == nil {
		p.in = strings.NewReader("")
	}
	rs, err := runContainerAction(context.Background(), f, "https://nas.example.com:5001", action, names, p)
	return rs, out.String(), err
}

func TestContainerActionInOrder(t *testing.T) {
	f := newFakeOps()
	rs, out, err := runAction(t, f, "stop", []string{"worker-1", "web-app-1"}, actionPrompt{yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.acted, []string{"stop worker-1", "stop web-app-1"}) {
		t.Errorf("acted %v, want the order given", f.acted)
	}
	if len(rs) != 2 || rs[0].State != "exited" || rs[1].Name != "web-app-1" {
		t.Errorf("results = %+v", rs)
	}
	if !strings.Contains(out, "Stopping worker-1 ... done in") || !strings.Contains(out, ", exited.") {
		t.Errorf("progress = %q", out)
	}
}

func TestContainerActionAsks(t *testing.T) {
	tests := []struct {
		answer string
		acted  bool
	}{
		{"y\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"\n", false},
		{"", false},
	}
	for _, tt := range tests {
		f := newFakeOps()
		_, out, err := runAction(t, f, "restart", []string{"web-app-1"}, actionPrompt{in: strings.NewReader(tt.answer), terminal: true})
		if !strings.HasPrefix(out, "Restart web-app-1 on https://nas.example.com:5001? [y/N] ") {
			t.Errorf("%q: prompt = %q", tt.answer, out)
		}
		if got := len(f.acted) > 0; got != tt.acted {
			t.Errorf("%q: acted = %v, want %v", tt.answer, got, tt.acted)
		}
		if !tt.acted && !errors.Is(err, errCanceled) {
			t.Errorf("%q: err = %v, want canceled", tt.answer, err)
		}
	}
}

func TestContainerActionNeedsYesWithoutTerminal(t *testing.T) {
	f := newFakeOps()
	_, _, err := runAction(t, f, "stop", []string{"web-app-1"}, actionPrompt{terminal: false})
	if err == nil || !strings.Contains(err.Error(), "--yes") || len(f.acted) > 0 {
		t.Errorf("err = %v, acted = %v; want an error naming --yes and nothing done", err, f.acted)
	}
}

func TestContainerActionChecksNamesFirst(t *testing.T) {
	f := newFakeOps()
	_, _, err := runAction(t, f, "stop", []string{"web-app-1", "web"}, actionPrompt{yes: true})
	if err == nil || !strings.Contains(err.Error(), `did you mean web-app-1?`) {
		t.Errorf("err = %v, want a suggestion", err)
	}
	if len(f.acted) > 0 {
		t.Errorf("acted %v before checking every name", f.acted)
	}
}

func TestContainerActionStopsAtFailure(t *testing.T) {
	f := newFakeOps()
	f.states["web-app-1"] = "exited"
	rs, _, err := runAction(t, f, "start", []string{"old-job", "web-app-1"}, actionPrompt{yes: true})
	if err == nil || !strings.Contains(err.Error(), "network 3d0e not found") {
		t.Errorf("err = %v, want the reason Docker gave", err)
	}
	if !slices.Equal(f.acted, []string{"start old-job"}) {
		t.Errorf("acted %v, want to stop after the failure", f.acted)
	}
	if len(rs) != 1 || rs[0].Error == "" {
		t.Errorf("results = %+v, want the failure", rs)
	}
}
