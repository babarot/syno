package discover

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
	"github.com/miekg/dns"
)

// Service types DSM advertises over mDNS. Which ones appear depends on the
// file services enabled on the NAS, so we browse several.
var mdnsServices = []string{
	"_smb._tcp",
	"_afpovertcp._tcp",
	"_http._tcp",
	"_device-info._tcp",
}

// browseMDNS returns IPv4 addresses mapped to the advertised instance name.
func browseMDNS(ctx context.Context, timeout time.Duration) (map[netip.Addr]string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	hosts := map[netip.Addr]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	errs := make(chan error, len(mdnsServices))

	for _, svc := range mdnsServices {
		resolver, err := zeroconf.NewResolver(nil)
		if err != nil {
			return nil, err
		}
		entries := make(chan *zeroconf.ServiceEntry)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for e := range entries {
				name := strings.TrimSuffix(e.HostName, ".")
				name = strings.TrimSuffix(name, ".local")
				if name == "" {
					name = e.Instance
				}
				mu.Lock()
				for _, ip := range e.AddrIPv4 {
					if a, ok := netip.AddrFromSlice(ip.To4()); ok {
						hosts[a] = name
					}
				}
				mu.Unlock()
			}
		}()
		if err := resolver.Browse(ctx, svc, "local.", entries); err != nil {
			errs <- err
		}
	}

	<-ctx.Done()
	wg.Wait()
	close(errs)
	for err := range errs {
		return nil, err
	}
	return hosts, nil
}

// lookupName asks the host's own mDNS responder for its name with a unicast
// reverse (PTR) query. It fills in names that the multicast browse missed.
func lookupName(ip netip.Addr) string {
	arpa, err := dns.ReverseAddr(ip.String())
	if err != nil {
		return ""
	}
	m := new(dns.Msg)
	m.SetQuestion(arpa, dns.TypePTR)
	c := &dns.Client{Timeout: time.Second}
	r, _, err := c.Exchange(m, netip.AddrPortFrom(ip, 5353).String())
	if err != nil {
		return ""
	}
	for _, rr := range r.Answer {
		if ptr, ok := rr.(*dns.PTR); ok {
			return strings.TrimSuffix(strings.TrimSuffix(ptr.Ptr, "."), ".local")
		}
	}
	return ""
}
