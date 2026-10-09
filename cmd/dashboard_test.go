package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/babarot/syno/internal/doctor"
	"github.com/babarot/syno/internal/dsm"
)

// panelNAS answers the APIs of the storage and containers panels.
func panelNAS(t *testing.T) *dsm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		switch r.PostForm.Get("api") {
		case "SYNO.Storage.CGI.Storage":
			fmt.Fprint(w, `{"success":true,"data":{
				"storagePools":[{"id":"reuse_1","num_id":1,"status":"normal","device_type":"shr_with_1_disk_protect","disks":["sata1","nvme0n1"]}],
				"volumes":[{"vol_path":"/volume1","status":"normal","fs_type":"btrfs","pool_path":"reuse_1","size":{"total":"100","used":"97"}}],
				"disks":[
					{"id":"sata1","name":"Drive 1","device":"/dev/sata1","isSsd":false,"status":"normal","smart_status":"normal","temp":37,"unc":2,"used_by":"reuse_1","remain_life":{"value":-1}},
					{"id":"nvme0n1","name":"M.2 Drive 1","device":"/dev/nvme0n1","isSsd":true,"status":"normal","smart_status":"normal","temp":44,"unc":0,"used_by":"reuse_1","remain_life":{"value":87}}
				]}}`)
		case "SYNO.Storage.CGI.Smart":
			fmt.Fprint(w, `{"success":true,"data":{"healthInfo":{"overview":{"poweron":"9100"}}}}`)
		default:
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return dsm.New(srv.URL, "")
}

func TestReadPanelStorage(t *testing.T) {
	th := doctor.DefaultThresholds()
	out, err := readPanel(context.Background(), panelNAS(t), "storage", doctor.Options{}, th)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := out.(storagePanel)
	if !ok {
		t.Fatalf("got %T", out)
	}
	if len(st.Disks) != 2 || len(st.Volumes) != 1 || len(st.Pools) != 1 {
		t.Fatalf("got %+v", st)
	}
	hdd, ssd := st.Disks[0], st.Disks[1]
	if hdd.Type != "HDD" || hdd.BadSectors != 2 || hdd.LifePercent != nil || hdd.Pool != "Pool 1" {
		t.Errorf("Drive 1 = %+v", hdd)
	}
	if ssd.Type != "SSD" || ssd.LifePercent == nil || *ssd.LifePercent != 87 || ssd.PowerOnHours == nil || *ssd.PowerOnHours != 9100 {
		t.Errorf("M.2 Drive 1 = %+v", ssd)
	}
	if st.Thresholds.VolumeWarn != th.VolumeUsageWarn || st.Thresholds.DiskTempFail != th.DiskTempFail {
		t.Errorf("thresholds = %+v", st.Thresholds)
	}
}

func TestReadPanelWithoutContainerManager(t *testing.T) {
	out, err := readPanel(context.Background(), panelNAS(t), "containers", doctor.Options{}, doctor.Thresholds{})
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := out.(containersPanel); !ok || p.Installed || p.Containers == nil {
		t.Errorf("got %+v, want not installed with no containers", p)
	}
}
