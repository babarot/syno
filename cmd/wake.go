package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/babarot/syno/internal/config"
	"github.com/babarot/syno/internal/discover"
	"github.com/babarot/syno/internal/dsm"
	"github.com/babarot/syno/internal/wol"
)

func newWakeCmd() *cobra.Command {
	var (
		macs    []string
		wait    bool
		timeout time.Duration
	)

	c := &cobra.Command{
		Use:   "wake",
		Short: "Start the NAS with Wake-on-LAN",
		Long: `Start the NAS with a Wake-on-LAN magic packet, sent as a broadcast on the
local network. The MAC addresses come from --mac, then from the profile,
where syno login saves them, then from the ARP table of this machine.

Wake-on-LAN has to be on in DSM (Control Panel > Hardware & Power), and the
packet does not cross routers, so this machine has to be on the same network
as the NAS. No login is needed.`,
		Example: `  syno wake
  syno wake --wait
  syno wake --mac 00:11:32:12:34:56`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			var p *config.Profile
			if _, sel, err := selectProfile(); err == nil {
				p = sel
			} else if len(macs) == 0 {
				return err
			}

			var client *dsm.Client
			var hostIP net.IP
			if p != nil {
				client = dsm.New(p.URL, p.TLS.Pin)
				if dsmAnswers(ctx, client, 2*time.Second) {
					fmt.Fprintf(os.Stderr, "%s is already up.\n", p.URL)
					return nil
				}
				hostIP = resolveHost(ctx, p.URL)
			}

			targets, err := wakeTargets(macs, p, hostIP, discover.LookupMAC)
			if err != nil {
				return err
			}
			nets, err := wol.LocalNetworks()
			if err != nil {
				return err
			}
			if err := wol.Send(targets, wol.Broadcasts(hostIP, nets), wol.Port, 3); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Sent the Wake-on-LAN packet to %s.\n", strings.Join(targets, ", "))

			if !wait || client == nil {
				return nil
			}
			fmt.Fprintf(os.Stderr, "Waiting for DSM at %s ...", p.URL)
			took, err := waitForDSM(ctx, func(ctx context.Context) bool { return dsmAnswers(ctx, client, 3*time.Second) }, 3*time.Second, timeout)
			if err != nil {
				fmt.Fprintln(os.Stderr)
				if errors.Is(err, context.DeadlineExceeded) {
					return fmt.Errorf("DSM did not answer within %v: check that Wake-on-LAN is on in DSM and that this machine is on the same network as the NAS", timeout)
				}
				return err
			}
			fmt.Fprintf(os.Stderr, " up after %v.\n", took.Round(time.Second))
			return nil
		},
	}

	c.Flags().StringSliceVar(&macs, "mac", nil, "MAC address to wake, instead of the ones saved in the profile (repeatable)")
	c.Flags().BoolVar(&wait, "wait", false, "Wait until DSM answers")
	c.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "With --wait, how long to wait")

	return c
}

// dsmAnswers reports whether DSM answers SYNO.API.Info, which needs no login.
func dsmAnswers(ctx context.Context, c *dsm.Client, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, err := c.APIInfo(ctx, "SYNO.API.Info")
	return err == nil
}

// resolveHost returns the IPv4 address of the host in rawURL, or nil.
func resolveHost(ctx context.Context, rawURL string) net.IP {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		return ip.To4()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", u.Hostname())
	if err != nil || len(ips) == 0 {
		return nil
	}
	return ips[0]
}

// wakeTargets picks the MAC addresses to wake: --mac, then the profile, then
// the ARP table entry of the host, which stays a while after it stops.
func wakeTargets(flagMACs []string, p *config.Profile, hostIP net.IP, lookup func(netip.Addr) (string, error)) ([]string, error) {
	pick := flagMACs
	if len(pick) == 0 && p != nil {
		pick = p.MACs
	}
	if len(pick) == 0 && hostIP != nil {
		if ip, ok := netip.AddrFromSlice(hostIP); ok {
			if mac, err := lookup(ip.Unmap()); err == nil && mac != "" {
				pick = []string{mac}
			}
		}
	}
	if len(pick) == 0 {
		return nil, errors.New("no MAC address saved for the NAS: run `syno login` again while it is up, or pass --mac")
	}
	out := make([]string, 0, len(pick))
	for _, m := range pick {
		hw, err := net.ParseMAC(m)
		if err != nil || len(hw) != 6 {
			return nil, fmt.Errorf("invalid MAC address %q", m)
		}
		out = append(out, hw.String())
	}
	return out, nil
}

// waitForDSM asks up every interval until it answers or timeout passes, and
// returns how long it took.
func waitForDSM(ctx context.Context, up func(context.Context) bool, interval, timeout time.Duration) (time.Duration, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if up(ctx) {
			return time.Since(start), nil
		}
		select {
		case <-ctx.Done():
			return time.Since(start), ctx.Err()
		case <-time.After(interval):
		}
	}
}
