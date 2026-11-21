package netdiag

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
)

func dialErr(addr string, errno syscall.Errno) error {
	ap := netip.MustParseAddrPort(addr)
	return &url.Error{Op: "Post", URL: "https://" + addr + "/webapi/entry.cgi", Err: &net.OpError{
		Op: "dial", Net: "tcp", Addr: net.TCPAddrFromAddrPort(ap),
		Err: os.NewSyscallError("connect", errno),
	}}
}

func stub(t *testing.T, system string, connects bool) *int {
	t.Helper()
	calls := 0
	oldGOOS, oldProbe := goos, probe
	goos = system
	probe = func(context.Context, netip.AddrPort) bool { calls++; return connects }
	t.Cleanup(func() { goos, probe = oldGOOS, oldProbe })
	return &calls
}

func TestExplain(t *testing.T) {
	tests := []struct {
		name      string
		os        string
		err       error
		connects  bool
		wantHint  string
		wantProbe bool
	}{
		{"blocked on macOS", "darwin", dialErr("192.168.1.10:5001", syscall.EHOSTUNREACH), true, BlockedHint, true},
		{"host gone on macOS", "darwin", dialErr("192.168.1.10:5001", syscall.EHOSTUNREACH), false, UnreachableHint, true},
		{"link-local on macOS", "darwin", dialErr("169.254.1.2:5001", syscall.EHOSTUNREACH), true, BlockedHint, true},
		{"not a LAN address", "darwin", dialErr("100.100.1.2:5001", syscall.EHOSTUNREACH), true, UnreachableHint, false},
		{"linux", "linux", dialErr("192.168.1.10:5001", syscall.EHOSTUNREACH), true, UnreachableHint, false},
		{"other errors untouched", "darwin", dialErr("192.168.1.10:5001", syscall.ECONNREFUSED), true, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := stub(t, tt.os, tt.connects)
			got := Explain(context.Background(), tt.err)
			if !errors.Is(got, tt.err) {
				t.Errorf("Explain dropped the original error: %v", got)
			}
			if tt.wantHint == "" {
				if got.Error() != tt.err.Error() {
					t.Errorf("got %v, want the error unchanged", got)
				}
			} else if !strings.HasSuffix(got.Error(), "; "+tt.wantHint) {
				t.Errorf("got %v, want hint %q", got, tt.wantHint)
			}
			if (*calls > 0) != tt.wantProbe {
				t.Errorf("probe calls = %d, want probe %v", *calls, tt.wantProbe)
			}
		})
	}
}

func TestUnreachable(t *testing.T) {
	ap, ok := Unreachable(dialErr("192.168.1.10:5001", syscall.EHOSTUNREACH))
	if !ok || ap != netip.MustParseAddrPort("192.168.1.10:5001") {
		t.Errorf("got %v %v", ap, ok)
	}
	if _, ok := Unreachable(errors.New("no route to host")); ok {
		t.Error("matched an error that has no address")
	}
}
