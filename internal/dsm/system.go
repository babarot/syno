package dsm

import (
	"context"
)

// SystemInfo is SYNO.Core.System info.
type SystemInfo struct {
	Model       string `json:"model"`
	Serial      string `json:"serial"`
	FirmwareVer string `json:"firmware_ver"`
	UpTime      string `json:"up_time"` // "h:m:s", hours may exceed 24
	SysTemp     Num    `json:"sys_temp"`
	// DSM's own judgement of the temperature.
	SysTempWarn        bool   `json:"sys_tempwarn"`
	TemperatureWarning bool   `json:"temperature_warning"`
	RAMSize            Num    `json:"ram_size"` // MB
	CPUVendor          string `json:"cpu_vendor"`
	CPUFamily          string `json:"cpu_family"`
	CPUSeries          string `json:"cpu_series"`
	CPUCores           string `json:"cpu_cores"`
	Time               string `json:"time"`
}

func (c *Client) SystemInfo(ctx context.Context) (*SystemInfo, error) {
	var out SystemInfo
	if err := c.Call(ctx, "SYNO.Core.System", 1, "info", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Utilization is SYNO.Core.System.Utilization get.
type Utilization struct {
	CPU struct {
		UserLoad   Num `json:"user_load"`
		SystemLoad Num `json:"system_load"`
		OtherLoad  Num `json:"other_load"`
	} `json:"cpu"`
	Memory struct {
		RealUsage Num `json:"real_usage"` // percent
		TotalReal Num `json:"total_real"` // KB
		AvailReal Num `json:"avail_real"` // KB
	} `json:"memory"`
	Network []struct {
		Device string `json:"device"`
		RX     Num    `json:"rx"` // bytes/s
		TX     Num    `json:"tx"` // bytes/s
	} `json:"network"`
}

func (c *Client) Utilization(ctx context.Context) (*Utilization, error) {
	var out Utilization
	if err := c.Call(ctx, "SYNO.Core.System.Utilization", 1, "get", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
