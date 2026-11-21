// Package netdiag explains why the NAS could not be reached, for errors
// that look the same whatever the cause.
package netdiag

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// Hints for a host that was unreachable (EHOSTUNREACH).
const (
	// BlockedHint is for a host that another program can reach while syno
	// cannot. macOS Local Network privacy denies it without a prompt when
	// the app responsible for syno, such as the terminal that started a
	// long-running multiplexer server, is not allowed or no longer running.
	BlockedHint = "the NAS answers /usr/bin/nc, but macOS Local Network privacy blocks syno from reaching it; " +
		"run syno in a terminal app allowed under System Settings > Privacy & Security > Local Network, " +
		"or restart the long-running process that started it, such as a terminal multiplexer's server"
	// UnreachableHint is for every other case.
	UnreachableHint = "the NAS may be off or asleep, or its IP address may have changed; check with syno discover"
)

// Unreachable returns the address that err failed to connect to with
// EHOSTUNREACH ("no route to host").
func Unreachable(err error) (netip.AddrPort, bool) {
	if !errors.Is(err, syscall.EHOSTUNREACH) {
		return netip.AddrPort{}, false
	}
	oe, ok := errors.AsType[*net.OpError](err)
	if !ok || oe.Addr == nil {
		return netip.AddrPort{}, false
	}
	ap, err := netip.ParseAddrPort(oe.Addr.String())
	if err != nil {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()), true
}

// Explain adds a hint to err when it failed to reach a host, and returns
// any other error as it is.
func Explain(ctx context.Context, err error) error {
	ap, ok := Unreachable(err)
	if !ok {
		return err
	}
	return fmt.Errorf("%w; %s", err, Hint(ctx, ap))
}

// Hint says why ap may have been unreachable.
func Hint(ctx context.Context, ap netip.AddrPort) string {
	if Blocked(ctx, ap) {
		return BlockedHint
	}
	return UnreachableHint
}

// Blocked reports whether macOS Local Network privacy is what keeps this
// process from ap. EHOSTUNREACH also means a host that is off or has moved,
// so this asks /usr/bin/nc to connect too: Apple's own binaries are exempt
// from the privacy check, so nc getting through while we did not points at
// the check and nothing else. It answers false when it cannot tell.
func Blocked(ctx context.Context, ap netip.AddrPort) bool {
	if goos != "darwin" || !isLocal(ap.Addr()) {
		return false
	}
	return probe(ctx, ap)
}

func isLocal(a netip.Addr) bool {
	return a.IsPrivate() || a.IsLinkLocalUnicast()
}

// Variables so that tests can stand in for them.
var (
	goos  = runtime.GOOS
	probe = ncConnects
)

// ncPath is absolute so that another nc on PATH, which is not exempt from
// the privacy check, is never used.
const ncPath = "/usr/bin/nc"

const ncTimeout = 2 * time.Second

// ncConnects reports whether nc can open a TCP connection to ap. A missing
// nc counts as no.
func ncConnects(ctx context.Context, ap netip.AddrPort) bool {
	ctx, cancel := context.WithTimeout(ctx, ncTimeout+time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ncPath, "-z", "-G", strconv.Itoa(int(ncTimeout/time.Second)),
		ap.Addr().String(), strconv.Itoa(int(ap.Port())))
	return cmd.Run() == nil
}
