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
