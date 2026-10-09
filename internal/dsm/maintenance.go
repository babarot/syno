package dsm

import (
	"context"
	"encoding/json"
	"time"
)

// NeedReboot reports whether DSM waits for a reboot to finish an update.
func (c *Client) NeedReboot(ctx context.Context) (bool, error) {
	var out struct {
		NeedReboot bool `json:"need_reboot"`
	}
	err := c.Call(ctx, "SYNO.Core.Hardware.NeedReboot", 1, "get", nil, &out)
	return out.NeedReboot, err
}

// UpgradeCheck is SYNO.Core.Upgrade.Server check (version 2 and later).
type UpgradeCheck struct {
	Update struct {
		Available      bool   `json:"available"`
		Version        string `json:"version"` // e.g. "DSM 7.2.2-72806"
		VersionDetails struct {
			IsSecurityVersion bool `json:"isSecurityVersion"`
		} `json:"version_details"`
	} `json:"update"`
}

// CheckUpgrade asks Synology's update server, through the NAS, whether a
// newer DSM is available. It needs the NAS to reach the internet.
func (c *Client) CheckUpgrade(ctx context.Context) (*UpgradeCheck, error) {
	var out UpgradeCheck
	if err := c.Call(ctx, "SYNO.Core.Upgrade.Server", 2, "check", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SecurityScan is SYNO.Core.SecurityScan.Status system_get, the result of
// the Security Advisor.
type SecurityScan struct {
	SysStatus    string `json:"sysStatus"` // e.g. "safe"
	LastScanTime Num    `json:"lastScanTime"`
	Items        map[string]struct {
		Category     string         `json:"category"`
		FailSeverity string         `json:"failSeverity"` // e.g. "safe"
		Fail         map[string]int `json:"fail"`         // count per severity
	} `json:"items"`
}

func (c *Client) SecurityScan(ctx context.Context) (*SecurityScan, error) {
	var out SecurityScan
	if err := c.Call(ctx, "SYNO.Core.SecurityScan.Status", 1, "system_get", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Certificate is one entry of SYNO.Core.Certificate.CRT list.
type Certificate struct {
	ID        string `json:"id"`
	Desc      string `json:"desc"`
	IsDefault bool   `json:"is_default"`
	IsBroken  bool   `json:"is_broken"`
	// Renewable is true for certificates DSM renews by itself (Let's Encrypt).
	Renewable bool   `json:"renewable"`
	ValidTill string `json:"valid_till"` // e.g. "Apr 10 05:39:01 2025 GMT"
	Subject   struct {
		CommonName string `json:"common_name"`
	} `json:"subject"`
	Services []json.RawMessage `json:"services"`
}

// Expiry parses ValidTill.
func (c Certificate) Expiry() (time.Time, error) {
	return time.Parse("Jan _2 15:04:05 2006 MST", c.ValidTill)
}

func (c *Client) Certificates(ctx context.Context) ([]Certificate, error) {
	var out struct {
		Certificates []Certificate `json:"certificates"`
	}
	if err := c.Call(ctx, "SYNO.Core.Certificate.CRT", 1, "list", nil, &out); err != nil {
		return nil, err
	}
	return out.Certificates, nil
}
