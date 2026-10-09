package dsm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiskLifePercent(t *testing.T) {
	var disks []Disk
	if err := json.Unmarshal([]byte(`[
		{"name": "Drive 1", "remain_life": {"trustable": true, "value": -1}},
		{"name": "M.2 Drive 1", "remain_life": {"trustable": true, "value": 87}},
		{"name": "Drive 2"}
	]`), &disks); err != nil {
		t.Fatal(err)
	}
	if _, ok := disks[0].LifePercent(); ok {
		t.Error("an HDD without an estimate has a life")
	}
	if v, ok := disks[1].LifePercent(); !ok || v != 87 {
		t.Errorf("SSD life = %v, %v, want 87, true", v, ok)
	}
	// A DSM that does not send remain_life must not read as 0% left.
	if v, ok := disks[2].LifePercent(); ok {
		t.Errorf("missing remain_life gives %v", v)
	}
}

func TestDiskHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("api") + " " + r.PostForm.Get("method"); got != "SYNO.Storage.CGI.Smart get_health_info" {
			t.Errorf("called %s", got)
		}
		if got := r.PostForm.Get("device"); got != `"/dev/sata1"` {
			t.Errorf("device = %q", got)
		}
		_, _ = w.Write([]byte(`{"success":true,"data":{"count":0,"healthInfo":{"overview":{"poweron":"23219","smart":"normal"}}}}`))
	}))
	defer srv.Close()

	h, err := NewInsecure(srv.URL).DiskHealth(context.Background(), "/dev/sata1")
	if err != nil {
		t.Fatal(err)
	}
	if h.PowerOnHours != 23219 {
		t.Errorf("power-on hours = %v, want 23219", h.PowerOnHours)
	}
}
