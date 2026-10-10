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

func TestRAIDName(t *testing.T) {
	tests := map[string]string{
		"shr_with_1_disk_protect":  "SHR",
		"shr_with_2_disk_protect":  "SHR-2",
		"shr_without_disk_protect": "SHR (no protection)",
		"raid_5":                   "RAID 5",
		"raid_10":                  "RAID 10",
		"raid_f1":                  "RAID F1",
		"raid_linear":              "JBOD",
		"basic":                    "Basic",
		"something_new":            "something_new",
	}
	for in, want := range tests {
		if got := RAIDName(in); got != want {
			t.Errorf("RAIDName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUtilizationIO(t *testing.T) {
	var u Utilization
	if err := json.Unmarshal([]byte(`{
		"space": {"volume": [{"device": "dm-1", "display_name": "volume1", "read_byte": 5421465, "write_byte": 0, "utilization": 9}]},
		"disk": {"disk": [{"device": "sata2", "display_name": "Drive 2", "read_byte": 1805516, "write_byte": 16384, "utilization": 6}]}
	}`), &u); err != nil {
		t.Fatal(err)
	}
	if v, ok := u.VolumeIO("/volume1"); !ok || v.ReadBytes != 5421465 || v.Utilization != 9 {
		t.Errorf("VolumeIO(/volume1) = %+v, %v", v, ok)
	}
	if _, ok := u.VolumeIO("/volume2"); ok {
		t.Error("found a volume DSM did not report")
	}
	if d, ok := u.DiskIO("sata2"); !ok || d.WriteBytes != 16384 || d.Utilization != 6 {
		t.Errorf("DiskIO(sata2) = %+v, %v", d, ok)
	}
}
