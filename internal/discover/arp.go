package discover

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// readARP returns the system ARP table as IP -> normalized MAC
// (lowercase, zero-padded, colon separated).
func readARP() (map[netip.Addr]string, error) {
	if runtime.GOOS == "linux" {
		return readProcARP()
	}
	out, err := exec.Command("arp", "-an").Output()
	if err != nil {
		return nil, fmt.Errorf("arp -an: %w", err)
	}
	// ? (192.168.1.10) at 90:9:d0:12:34:56 on en0 ifscope [ethernet]
	table := map[netip.Addr]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[2] != "at" {
			continue
		}
		ip, err := netip.ParseAddr(strings.Trim(f[1], "()"))
		if err != nil {
			continue
		}
		if mac := normalizeMAC(f[3]); mac != "" {
			table[ip] = mac
		}
	}
	return table, nil
}

func readProcARP() (map[netip.Addr]string, error) {
	b, err := os.ReadFile("/proc/net/arp")
	if err != nil {
		return nil, err
	}
	table := map[netip.Addr]string{}
	for _, line := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		ip, err := netip.ParseAddr(f[0])
		if err != nil {
			continue
		}
		if mac := normalizeMAC(f[3]); mac != "" && mac != "00:00:00:00:00:00" {
			table[ip] = mac
		}
	}
	return table, nil
}

// normalizeMAC turns "90:9:d0:12:34:56" into "90:09:d0:12:34:56".
func normalizeMAC(s string) string {
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return ""
	}
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = "0" + p
		}
	}
	hw, err := net.ParseMAC(strings.Join(parts, ":"))
	if err != nil {
		return ""
	}
	return hw.String()
}

// LookupMAC returns the MAC address the system ARP table has for ip, or ""
// when it has none. Entries expire, so this finds a host that stopped only
// a short while ago.
func LookupMAC(ip netip.Addr) (string, error) {
	table, err := readARP()
	if err != nil {
		return "", err
	}
	return table[ip.Unmap()], nil
}
