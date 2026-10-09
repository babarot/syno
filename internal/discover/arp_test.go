package discover

import "testing"

func TestNormalizeMAC(t *testing.T) {
	tests := map[string]string{
		"90:9:d0:12:34:56":  "90:09:d0:12:34:56",
		"00:11:32:AB:CD:EF": "00:11:32:ab:cd:ef",
		"(incomplete)":      "",
		"1:2:3":             "",
	}
	for in, want := range tests {
		if got := normalizeMAC(in); got != want {
			t.Errorf("normalizeMAC(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsSynologyMAC(t *testing.T) {
	if !isSynologyMAC("90:09:d0:12:34:56") {
		t.Error("90:09:d0 should be Synology")
	}
	if isSynologyMAC("a4:83:e7:00:00:01") {
		t.Error("a4:83:e7 should not be Synology")
	}
}
