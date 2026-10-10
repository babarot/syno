package dsm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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

func TestBays(t *testing.T) {
	var st Storage
	if err := json.Unmarshal([]byte(`{
		"env": {"bay_number": "4"},
		"disks": [
			{"id": "sata1", "device": "/dev/sata1", "slot_id": 1, "container": {"type": "internal"}},
			{"id": "sata3", "device": "/dev/sata3", "slot_id": 3, "container": {"type": "internal"}},
			{"id": "nvme0n1", "device": "/dev/nvme0n1", "slot_id": 2, "container": {"type": "internal"}},
			{"id": "sata5", "device": "/dev/sata5", "slot_id": 4, "container": {"type": "ebox"}}
		]}`), &st); err != nil {
		t.Fatal(err)
	}
	// The M.2 SSD and the disk of the expansion unit do not fill bays 2
	// and 4 of the NAS.
	total, empty, ok := st.Bays()
	if !ok || total != 4 || !slices.Equal(empty, []int{2, 4}) {
		t.Errorf("Bays() = %d, %v, %v, want 4, [2 4], true", total, empty, ok)
	}

	if _, _, ok := (&Storage{}).Bays(); ok {
		t.Error("a DSM that does not send bay_number has bays")
	}
}
