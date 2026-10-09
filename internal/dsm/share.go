package dsm

import (
	"context"
	"net/url"
)

// Share is one entry of SYNO.Core.Share list with the additional fields
// syno asks for. DSM puts them on the share itself, not under "additional".
type Share struct {
	Name    string `json:"name"`
	VolPath string `json:"vol_path"`
	Desc    string `json:"desc"`
	// QuotaUsed is the space the share uses, in MiB. It may have decimals.
	QuotaUsed Num  `json:"share_quota_used"`
	Hidden    bool `json:"hidden"`
	// Encryption is 0 for a share that is not encrypted.
	Encryption      Num  `json:"encryption"`
	RecycleBin      bool `json:"enable_recycle_bin"`
	IsForceReadonly bool `json:"is_force_readonly"`
	IsUSBShare      bool `json:"is_usb_share"`
}

// Shares lists the shared folders with their usage.
func (c *Client) Shares(ctx context.Context) ([]Share, error) {
	params := url.Values{"additional": {`["share_quota","encryption","hidden","recyclebin","is_force_readonly","is_usb_share"]`}}
	var out struct {
		Shares []Share `json:"shares"`
	}
	if err := c.Call(ctx, "SYNO.Core.Share", 1, "list", params, &out); err != nil {
		return nil, err
	}
	return out.Shares, nil
}
