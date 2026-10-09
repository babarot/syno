package dsm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"4.3.1-0530", "4.2.3-0522", 1, true},
		{"10.11.11-1551", "10.11.6-1369", 1, true}, // not a string comparison
		{"4.1.2-4045", "4.1.2-4039", 1, true},
		{"1.0.0-00502", "1.0.0-502", 0, true},
		{"1.58.2-700058002", "1.58.2-700058002", 0, true},
		{"1.2.3-0100", "1.2.3-0200", -1, true},
		{"1.2.3", "1.2.3-0001", -1, true},
		{"1.0-beta", "1.0-alpha", 1, true},
		{"1.0-beta", "1.0-0001", 0, false},
		{"", "1.0", 0, false},
	}
	for _, tt := range tests {
		got, ok := CompareVersions(tt.a, tt.b)
		if got != tt.want || ok != tt.ok {
			t.Errorf("CompareVersions(%q, %q) = %d, %v, want %d, %v", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}

func TestPackageUpdates(t *testing.T) {
	installed := []Package{
		{ID: "web", Version: "1.2.0-0100"},
		{ID: "Db", Version: "10.11.6-1369"},
		{ID: "agent", Version: "2.0.0-0001"},
		{ID: "thirdparty", Version: "1.0.0-1"},
		{ID: "beta", Version: "3.0.0-0300"},
	}
	store := []StorePackage{
		{ID: "web", Version: "1.3.0-0110"},
		{ID: "Db", Version: "10.11.11-1551", IsSecurityVersion: true},
		{ID: "agent", Version: "2.0.0-0001", IsSecurityVersion: true},
		{ID: "beta", Version: "2.9.0-0290"}, // older than installed
		{ID: "notinstalled", Version: "1.0.0-1"},
	}
	got := PackageUpdates(installed, store)

	want := []struct {
		id       string
		inStore  bool
		latest   string
		security bool
	}{
		{"agent", true, "", false},
		{"beta", true, "", false},
		{"Db", true, "10.11.11-1551", true},
		{"thirdparty", false, "", false},
		{"web", true, "1.3.0-0110", false},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d packages, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.ID != w.id || g.InStore != w.inStore || g.Latest != w.latest || g.Security != w.security {
			t.Errorf("[%d] = {%s in_store=%v latest=%q security=%v}, want %+v", i, g.ID, g.InStore, g.Latest, g.Security, w)
		}
		if g.Available() != (w.latest != "") {
			t.Errorf("%s: Available() = %v", g.ID, g.Available())
		}
	}
}

func TestPackageAPIs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		switch api := r.PostForm.Get("api"); api {
		case "SYNO.Core.Package":
			if got := r.PostForm.Get("additional"); got != `["status","install_type"]` {
				t.Errorf("additional = %q", got)
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"packages":[
				{"id": "web", "name": "Web", "version": "1.2.0-0100", "additional": {"status": "running", "install_type": "system"}}
			],"total":1}}`))
		case "SYNO.Core.Package.Server":
			// Version 1 answers without the packages.
			if got := r.PostForm.Get("version"); got != "2" {
				t.Errorf("Package.Server version = %s, want 2", got)
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"packages":[
				{"id": "web", "version": "1.3.0-0110", "is_security_version": true, "dname": "Web"}
			],"beta_packages":[],"categories":[],"banners":[]}}`))
		default:
			t.Errorf("unexpected API %s", api)
		}
	}))
	defer srv.Close()

	c := NewInsecure(srv.URL)
	ps, err := c.Packages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].Name != "Web" || ps[0].Additional.Status != "running" || ps[0].Additional.InstallType != "system" {
		t.Errorf("Packages = %+v", ps)
	}
	ss, err := c.StorePackages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 1 || ss[0].Version != "1.3.0-0110" || !ss[0].IsSecurityVersion {
		t.Errorf("StorePackages = %+v", ss)
	}
}
