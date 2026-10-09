package discover

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"time"
)

// sweep tries to connect to the DSM ports on every host of the local /24
// subnets and returns the hosts that accept a connection.
func sweep(ctx context.Context) []netip.Addr {
	var targets []netip.Addr
	for _, p := range localPrefixes() {
		for a := p.Masked().Addr().Next(); p.Contains(a); a = a.Next() {
			targets = append(targets, a)
		}
	}

	var (
		mu    sync.Mutex
		found []netip.Addr
		wg    sync.WaitGroup
		sem   = make(chan struct{}, 128)
	)
	d := net.Dialer{Timeout: 500 * time.Millisecond}
	for _, ip := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			for _, port := range []uint16{5000, 5001} {
				conn, err := d.DialContext(ctx, "tcp", netip.AddrPortFrom(ip, port).String())
				if err == nil {
					conn.Close()
					mu.Lock()
					found = append(found, ip)
					mu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	return found
}

// localPrefixes returns the /24 networks of the active private IPv4 interfaces.
// Larger networks are narrowed to the /24 around our own address to keep the
// sweep short.
func localPrefixes() []netip.Prefix {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[netip.Prefix]bool{}
	var out []netip.Prefix
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(ipn.IP.To4())
			if !ok || !ip.IsPrivate() {
				continue
			}
			ones, _ := ipn.Mask.Size()
			if ones < 24 {
				ones = 24
			}
			p := netip.PrefixFrom(ip, ones).Masked()
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}
