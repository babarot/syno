package cmd

import "testing"

func TestDefaultProfileName(t *testing.T) {
	tests := []struct {
		mdns, url, want string
	}{
		{"nas", "https://192.168.1.10:5001", "nas"},
		{"", "https://192.168.1.10:5001", "192.168.1.10"},
		{"", "https://nas.example.com:5001", "nas.example.com"},
		{"", "", "default"},
	}
	for _, tt := range tests {
		if got := defaultProfileName(tt.mdns, tt.url); got != tt.want {
			t.Errorf("defaultProfileName(%q, %q) = %q, want %q", tt.mdns, tt.url, got, tt.want)
		}
	}
}
