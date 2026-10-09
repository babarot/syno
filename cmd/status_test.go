package cmd

import "testing"

func TestFormatUptime(t *testing.T) {
	tests := map[string]string{
		"1234:5:6": "51d 10h 5m",
		"0:42:00":  "0d 0h 42m",
		"bogus":    "bogus",
	}
	for in, want := range tests {
		if got := formatUptime(in); got != want {
			t.Errorf("formatUptime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := map[float64]string{
		512:           "512 B",
		1536:          "1.5 KB",
		3999969443840: "3.6 TB",
	}
	for in, want := range tests {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%v) = %q, want %q", in, got, want)
		}
	}
}
