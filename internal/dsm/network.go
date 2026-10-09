package dsm

import (
	"context"
	"net/url"
)

// NetworkInterface is one entry of nif in SYNO.Core.System info with
// type=network.
type NetworkInterface struct {
	ID     string `json:"id"` // e.g. "eth0"
	MAC    string `json:"mac"`
	Addr   string `json:"addr"`
	Status string `json:"status"`
}

// NetworkInterfaces lists the network interfaces of the NAS with their MAC
// addresses, which SYNO.Core.Network.Ethernet does not report.
func (c *Client) NetworkInterfaces(ctx context.Context) ([]NetworkInterface, error) {
	var out struct {
		NIF []NetworkInterface `json:"nif"`
	}
	params := url.Values{"type": {`"network"`}}
	if err := c.Call(ctx, "SYNO.Core.System", 1, "info", params, &out); err != nil {
		return nil, err
	}
	return out.NIF, nil
}

// WakeOnLANEnabled reports whether Wake-on-LAN is on for any LAN port
// (Control Panel > Hardware & Power).
func (c *Client) WakeOnLANEnabled(ctx context.Context) (bool, error) {
	var out struct {
		WOL []struct {
			Enable bool `json:"enable"`
			Idx    int  `json:"idx"`
		} `json:"wol"`
	}
	if err := c.Call(ctx, "SYNO.Core.Hardware.PowerRecovery", 1, "get", nil, &out); err != nil {
		return false, err
	}
	for _, w := range out.WOL {
		if w.Enable {
			return true, nil
		}
	}
	return false, nil
}
