package cmd

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/babarot/syno/internal/dsm"
)

func testPackageUpdates() []dsm.PackageUpdate {
	pkg := func(id, name, version, status string) dsm.Package {
		p := dsm.Package{ID: id, Name: name, Version: version}
		p.Additional.Status = status
		return p
	}
	return []dsm.PackageUpdate{
		{Package: pkg("Db", "Database", "10.11.6-1369", "stop"), InStore: true, Latest: "10.11.11-1551", Security: true},
		{Package: pkg("files", "Files", "1.4.2-1575", "running"), InStore: true},
		{Package: pkg("tool", "Tool", "2.0.0-1", "running")},
		{Package: pkg("web", "Web", "1.2.0-0100", "running"), InStore: true, Latest: "1.3.0-0110"},
	}
}

func TestPrintPackages(t *testing.T) {
	var buf bytes.Buffer
	if err := printPackages(&buf, packageRows(testPackageUpdates(), false)); err != nil {
		t.Fatal(err)
	}
	want := "" +
		"ID      NAME       VERSION        LATEST                     STATUS\n" +
		"Db      Database   10.11.6-1369   10.11.11-1551 (security)   stop\n" +
		"files   Files      1.4.2-1575     -                          running\n" +
		"tool    Tool       2.0.0-1        ?                          running\n" +
		"web     Web        1.2.0-0100     1.3.0-0110                 running\n"
	if got := buf.String(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestPackageRowsOutdated(t *testing.T) {
	rows := packageRows(testPackageUpdates(), true)
	if len(rows) != 2 || rows[0].ID != "Db" || rows[1].ID != "web" {
		t.Errorf("outdated rows = %+v", rows)
	}
}

func TestPackageRowsJSON(t *testing.T) {
	b, err := json.Marshal(packageRows(testPackageUpdates(), false)[:3])
	if err != nil {
		t.Fatal(err)
	}
	want := `[` +
		`{"id":"Db","name":"Database","version":"10.11.6-1369","latest":"10.11.11-1551","security":true,"in_store":true,"status":"stop"},` +
		`{"id":"files","name":"Files","version":"1.4.2-1575","security":false,"in_store":true,"status":"running"},` +
		`{"id":"tool","name":"Tool","version":"2.0.0-1","security":false,"in_store":false,"status":"running"}]`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}
