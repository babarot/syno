package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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
	if err := printShares(&buf, shareRows(testShares()), false); err != nil {
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

type fakeMeasurer struct {
	mu    sync.Mutex
	paths []string
	sizes map[string]float64
	slow  string // a path that never finishes
}

func (f *fakeMeasurer) MeasureDir(ctx context.Context, path string, _ time.Duration) (*dsm.DirSize, error) {
	f.mu.Lock()
	f.paths = append(f.paths, path)
	f.mu.Unlock()
	if path == f.slow {
		<-ctx.Done()
		return &dsm.DirSize{NumFile: 42}, ctx.Err()
	}
	return &dsm.DirSize{Finished: true, TotalSize: dsm.Num(f.sizes[path])}, nil
}

func TestMeasureRecycleBins(t *testing.T) {
	rows := shareRows(testShares())
	m := &fakeMeasurer{sizes: map[string]float64{"/media/#recycle": 5 << 30}}
	if err := measureRecycleBins(context.Background(), m, rows); err != nil {
		t.Fatal(err)
	}
	// DirSize sums a missing folder to 0, so only shares with a recycle bin
	// are asked about.
	if !slices.Equal(m.paths, []string{"/media/#recycle"}) {
		t.Errorf("measured %v, want only the share with a recycle bin", m.paths)
	}

	var buf bytes.Buffer
	if err := printShares(&buf, rows, true); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"NAME     VOLUME     USED     RECYCLE BIN   FLAGS\n" +
		"media    /volume1   3.0 TB   5.0 GB        recycle-bin\n" +
		"backup   /volume2   2.0 GB   -             encrypted,read-only,usb\n" +
		"photo    /volume1   2.0 GB   -             \n" +
		"app      /volume1   0 B      -             hidden\n"
	if got := buf.String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}

	b, err := json.Marshal(rows[:2])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"recycle_bin":true,"recycle_bin_bytes":5368709120}`) || strings.Count(string(b), "recycle_bin_bytes") != 1 {
		t.Errorf("JSON = %s, want recycle_bin_bytes only where it was summed", b)
	}
}

func TestMeasureRecycleBinsGivesUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	rows := shareRows(testShares())
	m := &fakeMeasurer{slow: "/media/#recycle"}
	err := measureRecycleBins(ctx, m, rows)
	if err == nil || !strings.Contains(err.Error(), "media (42 files so far)") {
		t.Errorf("err = %v, want the share that did not finish", err)
	}
}
