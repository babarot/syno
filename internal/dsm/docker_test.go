package dsm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// containersResponse has the shape of SYNO.Docker.Container list on DSM 7.2
// with Container Manager, trimmed to two containers.
const containersResponse = `{"success":true,"data":{"containers":[
	{
		"name": "web-app-1", "image": "example/app:latest", "status": "running", "up_status": "Up 2 days",
		"State": {"Status": "running", "Running": true, "ExitCode": 0, "Health": {"Status": "healthy", "FailingStreak": 0}},
		"Labels": {"com.docker.compose.project": "web", "com.docker.compose.service": "app"},
		"Env": ["SECRET=hunter2"]
	},
	{
		"name": "db", "image": "example/db:1", "status": "stopped", "up_status": "Exited (1) 3 hours ago",
		"State": {"Status": "exited", "Running": false, "ExitCode": 1},
		"Labels": {}
	}
],"limit":2,"offset":0,"total":2}}`

func TestContainers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		// The API fails with code 114 when any of these is missing.
		for k, want := range map[string]string{"limit": "-1", "offset": "0", "type": `"all"`} {
			if got := r.PostForm.Get(k); got != want {
				t.Errorf("%s = %q, want %q", k, got, want)
			}
		}
		_, _ = w.Write([]byte(containersResponse))
	}))
	defer srv.Close()

	cs, err := NewInsecure(srv.URL).Containers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d containers, want 2", len(cs))
	}

	app, db := cs[0], cs[1]
	if app.Name != "web-app-1" || app.State.Status != "running" || !app.State.Running || app.UpStatus != "Up 2 days" {
		t.Errorf("app = %+v", app)
	}
	if app.Health() != "healthy" || app.Project() != "web" {
		t.Errorf("app health %q, project %q", app.Health(), app.Project())
	}
	if db.Health() != "" || db.Project() != "" || db.State.ExitCode != 1 {
		t.Errorf("db health %q, project %q, exit code %d", db.Health(), db.Project(), db.State.ExitCode)
	}
}

func TestIsNoAPI(t *testing.T) {
	if !IsNoAPI(&APIError{API: ContainerAPI, Method: "list", Code: 102}, ContainerAPI) {
		t.Error("code 102 should be IsNoAPI")
	}
	if IsNoAPI(&APIError{API: ContainerAPI, Method: "list", Code: 103}, ContainerAPI) {
		t.Error("code 103 should not be IsNoAPI")
	}
	if IsNoAPI(&APIError{API: "SYNO.Core.System", Method: "info", Code: 102}, ContainerAPI) {
		t.Error("code 102 of another API should not be IsNoAPI")
	}
}

func TestContainerAction(t *testing.T) {
	var method, name string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		method, name = r.PostForm.Get("method"), r.PostForm.Get("name")
		_, _ = w.Write([]byte(`{"success":true,"data":{"name":"web-app-1","cpu":0.25,"memory":4878336,"memoryPercent":0.12}}`))
	}))
	defer srv.Close()
	c := NewInsecure(srv.URL)

	r, err := c.ContainerAction(context.Background(), "restart", "web-app-1")
	if err != nil {
		t.Fatal(err)
	}
	if method != "restart" || name != `"web-app-1"` {
		t.Errorf("sent method=%s name=%s, want restart and a JSON string", method, name)
	}
	if r.Memory != 4878336 {
		t.Errorf("result = %+v", r)
	}
	if _, err := c.ContainerAction(context.Background(), "delete", "web-app-1"); err == nil {
		t.Error("delete should be refused")
	}
}

func TestContainerStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{
			"5729f065": {"name": "/web-app-1", "read": "2026-10-09T15:51:49Z",
				"cpu_stats": {"cpu_usage": {"total_usage": 65579607}, "system_cpu_usage": 33289597340000000, "online_cpus": 4},
				"memory_stats": {"usage": 3993600, "limit": 4079349760, "stats": {"inactive_file": 20480}}},
			"6c06daff": {"name": "/old-job", "read": "0001-01-01T00:00:00Z",
				"cpu_stats": {"cpu_usage": {"total_usage": 0}, "system_cpu_usage": null, "online_cpus": null},
				"memory_stats": {"usage": null}}
		}}`))
	}))
	defer srv.Close()

	stats, err := NewInsecure(srv.URL).ContainerStats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	web, ok := stats["web-app-1"]
	if !ok {
		t.Fatalf("no web-app-1 in %v", stats)
	}
	if web.CPUStats.OnlineCPUs != 4 || web.MemoryBytes() != 3993600-20480 {
		t.Errorf("web-app-1 = %+v, memory %v", web, web.MemoryBytes())
	}
	if old := stats["old-job"]; old.MemoryBytes() != 0 {
		t.Errorf("old-job memory = %v, want 0", old.MemoryBytes())
	}
	if _, ok := CPUPercent(stats["old-job"], stats["old-job"]); ok {
		t.Error("a stopped container should have no CPU figure")
	}
}
