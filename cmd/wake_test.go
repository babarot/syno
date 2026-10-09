package cmd

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/babarot/syno/internal/config"
)

func TestWakeTargets(t *testing.T) {
	arp := func(ip netip.Addr) (string, error) {
		if ip == netip.MustParseAddr("192.168.1.10") {
			return "00:11:32:00:00:03", nil
		}
		return "", nil
	}
	saved := &config.Profile{MACs: []string{"00:11:32:00:00:01", "00:11:32:00:00:02"}}
	host := net.ParseIP("192.168.1.10").To4()
	tests := []struct {
		name  string
		flags []string
		p     *config.Profile
		ip    net.IP
		want  []string
	}{
		{"flag wins", []string{"00-11-32-00-00-09"}, saved, host, []string{"00:11:32:00:00:09"}},
		{"profile", nil, saved, host, []string{"00:11:32:00:00:01", "00:11:32:00:00:02"}},
		{"arp table", nil, &config.Profile{}, host, []string{"00:11:32:00:00:03"}},
		{"arp table without profile", nil, nil, host, []string{"00:11:32:00:00:03"}},
	}
	for _, tt := range tests {
		got, err := wakeTargets(tt.flags, tt.p, tt.ip, arp)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %v, %v, want %v", tt.name, got, err, tt.want)
		}
	}

	if _, err := wakeTargets(nil, &config.Profile{}, net.ParseIP("192.168.1.99"), arp); err == nil {
		t.Error("no MAC anywhere should be an error")
	}
	if _, err := wakeTargets([]string{"nope"}, nil, nil, arp); err == nil {
		t.Error("an invalid --mac should be an error")
	}
}

func TestWaitForDSM(t *testing.T) {
	calls := 0
	_, err := waitForDSM(context.Background(), func(context.Context) bool { calls++; return calls == 3 }, time.Millisecond, time.Second)
	if err != nil || calls != 3 {
		t.Errorf("err = %v after %d calls, want up on the third", err, calls)
	}

	_, err = waitForDSM(context.Background(), func(context.Context) bool { return false }, time.Millisecond, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the timeout", err)
	}
}
