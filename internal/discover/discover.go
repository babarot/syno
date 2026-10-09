package discover

import (
	"context"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Options controls how discovery runs.
type Options struct {
	// Timeout is how long to listen for mDNS responses.
	Timeout time.Duration
	// Scan enables a TCP sweep of the local subnets.
	Scan bool
}

// Device is a Synology NAS found on the network.
type Device struct {
	Name    string   `json:"name,omitempty"`
	IP      string   `json:"ip"`
	MAC     string   `json:"mac,omitempty"`
	DSMURL  string   `json:"dsm_url,omitempty"`
	Sources []string `json:"sources"`
}

// Synology's MAC address vendor prefixes (OUI).
var synologyOUIs = []string{"00:11:32", "90:09:d0"}

func isSynologyMAC(mac string) bool {
	for _, oui := range synologyOUIs {
		if strings.HasPrefix(mac, oui) {
			return true
		}
	}
	return false
}

type candidate struct {
	name    string
	sources []string
}

// Run discovers Synology devices.
func Run(ctx context.Context, opts Options) ([]Device, error) {
	cands := map[netip.Addr]*candidate{}
	add := func(ip netip.Addr, name, source string) {
		c, ok := cands[ip]
		if !ok {
			c = &candidate{}
			cands[ip] = c
		}
		if c.name == "" {
			c.name = name
		}
		if !slices.Contains(c.sources, source) {
			c.sources = append(c.sources, source)
		}
	}

	hosts, err := browseMDNS(ctx, opts.Timeout)
	if err != nil {
		return nil, err
	}
	for ip, name := range hosts {
		add(ip, name, "mdns")
	}

	if opts.Scan {
		for _, ip := range sweep(ctx) {
			add(ip, "", "scan")
		}
	}

	// Probe DSM on every candidate. Connecting also fills the ARP table,
	// so the MAC lookup below can see hosts we have not talked to before.
	dsm := map[netip.Addr]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for ip := range cands {
		wg.Go(func() {
			if u := probeDSM(ctx, ip); u != "" {
				mu.Lock()
				dsm[ip] = u
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	arp, err := readARP()
	if err != nil {
		return nil, err
	}
	for ip, mac := range arp {
		if isSynologyMAC(mac) {
			add(ip, "", "arp")
			if _, ok := dsm[ip]; !ok {
				if u := probeDSM(ctx, ip); u != "" {
					dsm[ip] = u
				}
			}
		}
	}

	var devices []Device
	for ip, c := range cands {
		mac := arp[ip]
		// Keep only hosts that look like Synology: a Synology MAC or a DSM endpoint.
		if !isSynologyMAC(mac) && dsm[ip] == "" {
			continue
		}
		if c.name == "" {
			c.name = lookupName(ip)
		}
		devices = append(devices, Device{
			Name:    c.name,
			IP:      ip.String(),
			MAC:     mac,
			DSMURL:  dsm[ip],
			Sources: c.sources,
		})
	}
	sort.Slice(devices, func(i, j int) bool {
		a, _ := netip.ParseAddr(devices[i].IP)
		b, _ := netip.ParseAddr(devices[j].IP)
		return a.Less(b)
	})
	return devices, nil
}
