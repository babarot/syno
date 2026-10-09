// Package wol sends Wake-on-LAN magic packets.
package wol

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strconv"
)

// Port is the port magic packets are sent to. Network cards look at any
// UDP packet, and 9 (discard) is the usual choice.
const Port = 9

// MagicPacket is 6 bytes of 0xFF followed by the MAC address 16 times.
func MagicPacket(mac string) ([]byte, error) {
	hw, err := net.ParseMAC(mac)
	if err != nil {
		return nil, err
	}
	if len(hw) != 6 {
		return nil, fmt.Errorf("%s is not a 48-bit MAC address", mac)
	}
	return append(bytes.Repeat([]byte{0xFF}, 6), bytes.Repeat(hw, 16)...), nil
}

// Broadcasts returns where to send a packet for a host: the limited
// broadcast address, and the broadcast address of each local network that
// contains the host's IP, when it is known.
func Broadcasts(hostIP net.IP, nets []*net.IPNet) []net.IP {
	out := []net.IP{net.IPv4bcast}
	ip4 := hostIP.To4()
	if ip4 == nil {
		return out
	}
	for _, n := range nets {
		if n.IP.To4() == nil || !n.Contains(ip4) {
			continue
		}
		b := directedBroadcast(n)
		if b != nil && !b.Equal(net.IPv4bcast) && !containsIP(out, b) {
			out = append(out, b)
		}
	}
	return out
}

func directedBroadcast(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	mask := n.Mask
	if len(mask) == net.IPv6len {
		mask = mask[12:]
	}
	if ip == nil || len(mask) != net.IPv4len {
		return nil
	}
	b := make(net.IP, net.IPv4len)
	for i := range b {
		b[i] = ip[i] | ^mask[i]
	}
	return b
}

func containsIP(ips []net.IP, ip net.IP) bool {
	for _, x := range ips {
		if x.Equal(ip) {
			return true
		}
	}
	return false
}

// LocalNetworks returns the IPv4 networks of the interfaces that are up.
func LocalNetworks() ([]*net.IPNet, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []*net.IPNet
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				out = append(out, n)
			}
		}
	}
	return out, nil
}

// Send sends the magic packet of each MAC address to each address, times
// times, since UDP does not tell whether a packet arrived.
func Send(macs []string, to []net.IP, port, times int) error {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return err
	}
	defer conn.Close()

	var sent int
	var errs []error
	for _, mac := range macs {
		p, err := MagicPacket(mac)
		if err != nil {
			return err
		}
		for _, ip := range to {
			addr := &net.UDPAddr{IP: ip, Port: port}
			for range times {
				if _, err := conn.WriteTo(p, addr); err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", net.JoinHostPort(ip.String(), strconv.Itoa(port)), err))
					continue
				}
				sent++
			}
		}
	}
	if sent == 0 {
		return errors.Join(errs...)
	}
	return nil
}
