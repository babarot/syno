package doctor

import (
	"testing"

	"github.com/babarot/syno/internal/dsm"
)

func TestPackageUpdate(t *testing.T) {
	installed := []dsm.Package{
		{ID: "web", Version: "1.2.0-0100"},
		{ID: "db", Version: "10.11.6-1369"},
		{ID: "tool", Version: "2.0.0-1"},
	}
	tests := []struct {
		name    string
		store   []dsm.StorePackage
		want    Level
		summary string
	}{
		{
			name:    "up to date",
			store:   []dsm.StorePackage{{ID: "web", Version: "1.2.0-0100"}, {ID: "db", Version: "10.11.6-1369", IsSecurityVersion: true}},
			want:    OK,
			summary: "packages are up to date",
		},
		{
			name:    "updates without security",
			store:   []dsm.StorePackage{{ID: "web", Version: "1.3.0-0110"}, {ID: "db", Version: "10.11.11-1551"}},
			want:    OK,
			summary: "2 updates available, none for security",
		},
		{
			name:    "security update",
			store:   []dsm.StorePackage{{ID: "web", Version: "1.3.0-0110"}, {ID: "db", Version: "10.11.11-1551", IsSecurityVersion: true}},
			want:    Warn,
			summary: "db 10.11.11-1551 is a security update (installed 10.11.6-1369)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := runSource(&fakeSource{pkgs: installed, store: tt.store})["package-update"]
			if r.Level != tt.want || r.Summary != tt.summary {
				t.Errorf("got %s %q, want %s %q", r.Level, r.Summary, tt.want, tt.summary)
			}
		})
	}
}
