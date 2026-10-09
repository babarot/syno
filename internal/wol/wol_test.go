package wol

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestMagicPacket(t *testing.T) {
	p, err := MagicPacket("00:11:32:aa:bb:cc")
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 102 {
		t.Fatalf("len = %d, want 102", len(p))
	}
	if !bytes.Equal(p[:6], bytes.Repeat([]byte{0xFF}, 6)) {
		t.Errorf("header = %x", p[:6])
	}
	mac := []byte{0x00, 0x11, 0x32, 0xaa, 0xbb, 0xcc}
	for i := range 16 {
		if got := p[6+i*6 : 12+i*6]; !bytes.Equal(got, mac) {
			t.Errorf("repetition %d = %x", i, got)
		}
	}
	for _, bad := range []string{"", "nope", "00:11:32:aa:bb", "00:00:5e:00:53:01:00:01"} {
		if _, err := MagicPacket(bad); err == nil {
			t.Errorf("MagicPacket(%q) should fail", bad)
		}
	}
}

func cidr(t *testing.T, s string) *net.IPNet {
	t.Helper()
	ip, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatal(err)
	}
	n.IP = ip // as net.Interface.Addrs reports it: the address, not the network
	return n
}

func TestBroadcasts(t *testing.T) {
	nets := []*net.IPNet{cidr(t, "192.168.1.37/24"), cidr(t, "10.20.3.4/16"), cidr(t, "100.64.0.5/32")}
	tests := []struct {
		host string
		want []string
	}{
		{"192.168.1.10", []string{"255.255.255.255", "192.168.1.255"}},
		{"10.20.200.1", []string{"255.255.255.255", "10.20.255.255"}},
		{"172.16.0.1", []string{"255.255.255.255"}}, // not on a local network
		{"", []string{"255.255.255.255"}},           // host name that did not resolve
	}
	for _, tt := range tests {
		var got []string
		for _, ip := range Broadcasts(net.ParseIP(tt.host), nets) {
			got = append(got, ip.String())
		}
		if len(got) != len(tt.want) {
			t.Errorf("%s: got %v, want %v", tt.host, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s: got %v, want %v", tt.host, got, tt.want)
			}
		}
	}
}

func TestSend(t *testing.T) {
	l, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	addr, ok := l.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("local address is %T", l.LocalAddr())
	}
	port := addr.Port

	if err := Send([]string{"00:11:32:aa:bb:cc"}, []net.IP{net.IPv4(127, 0, 0, 1)}, port, 3); err != nil {
		t.Fatal(err)
	}
	want, _ := MagicPacket("00:11:32:aa:bb:cc")
	buf := make([]byte, 200)
	for i := range 3 {
		_ = l.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := l.ReadFrom(buf)
		if err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		if !bytes.Equal(buf[:n], want) {
			t.Errorf("packet %d = %x", i, buf[:n])
		}
	}
}
