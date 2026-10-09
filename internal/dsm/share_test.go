package dsm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestShares(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if got := r.PostForm.Get("additional"); got != `["share_quota","encryption","hidden","recyclebin","is_force_readonly","is_usb_share"]` {
			t.Errorf("additional = %q", got)
		}
		// DSM puts the additional fields on each share, not under "additional".
		_, _ = w.Write([]byte(`{"success":true,"data":{"shares":[
			{"name": "media", "vol_path": "/volume1", "desc": "", "share_quota_used": 1048576.5, "quota_value": 0,
			 "hidden": false, "encryption": 0, "enable_recycle_bin": true, "is_force_readonly": false, "is_usb_share": false},
			{"name": "vault", "vol_path": "/volume2", "share_quota_used": 0.01171875,
			 "hidden": true, "encryption": 1, "enable_recycle_bin": false, "is_force_readonly": true, "is_usb_share": true}
		],"total":2}}`))
	}))
	defer srv.Close()

	ss, err := NewInsecure(srv.URL).Shares(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 2 {
		t.Fatalf("got %d shares, want 2", len(ss))
	}
	media, vault := ss[0], ss[1]
	if media.Name != "media" || media.VolPath != "/volume1" || media.QuotaUsed != 1048576.5 || !media.RecycleBin || media.Encryption != 0 {
		t.Errorf("media = %+v", media)
	}
	if !vault.Hidden || vault.Encryption == 0 || !vault.IsForceReadonly || !vault.IsUSBShare || vault.QuotaUsed != 0.01171875 {
		t.Errorf("vault = %+v", vault)
	}
}
