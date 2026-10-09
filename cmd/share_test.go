package cmd

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/babarot/syno/internal/dsm"
)

func testShares() []dsm.Share {
	return []dsm.Share{
		{Name: "photo", VolPath: "/volume1", QuotaUsed: 2048},
		{Name: "media", VolPath: "/volume1", QuotaUsed: 3 * 1024 * 1024, RecycleBin: true},
		{Name: "app", VolPath: "/volume1", QuotaUsed: 0, Hidden: true},
		{Name: "backup", VolPath: "/volume2", QuotaUsed: 2048, Encryption: 1, IsForceReadonly: true, IsUSBShare: true},
	}
}

func TestShareRows(t *testing.T) {
	rows := shareRows(testShares())
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	// The largest first, then by name.
	if got, want := names, []string{"media", "backup", "photo", "app"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
	if rows[0].UsedBytes != 3*1024*1024*1024*1024 {
		t.Errorf("media used_bytes = %v, want 3 TiB", rows[0].UsedBytes)
	}
}

func TestPrintShares(t *testing.T) {
	var buf bytes.Buffer
	if err := printShares(&buf, shareRows(testShares())); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"NAME     VOLUME     USED     FLAGS\n" +
		"media    /volume1   3.0 TB   recycle-bin\n" +
		"backup   /volume2   2.0 GB   encrypted,read-only,usb\n" +
		"photo    /volume1   2.0 GB   \n" +
		"app      /volume1   0 B      hidden\n"
	if got := buf.String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestShareRowsJSON(t *testing.T) {
	b, err := json.Marshal(shareList{Host: "https://nas.example.com:5001", Shares: shareRows(testShares()[3:])})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"host":"https://nas.example.com:5001","shares":[{"name":"backup","volume":"/volume2","used_bytes":2147483648,"hidden":false,"encrypted":true,"read_only":true,"usb":true,"recycle_bin":false}]}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}
