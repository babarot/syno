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
	// Space has the I/O of each volume, and Disk that of each disk. DSM
	// refreshes them every few seconds.
	Space struct {
		Volume []IOStat `json:"volume"`
	} `json:"space"`
	Disk struct {
		Disk []IOStat `json:"disk"`
	} `json:"disk"`
}

// IOStat is the I/O of a volume or a disk.
type IOStat struct {
	// Device is like "sata1" for a disk, and like "dm-1" for a volume.
	Device string `json:"device"`
	// DisplayName is like "Drive 1", or "volume1" for /volume1.
	DisplayName string `json:"display_name"`
	ReadBytes   Num    `json:"read_byte"`  // bytes/s
	WriteBytes  Num    `json:"write_byte"` // bytes/s
	// Utilization is the percent of the time the device was busy.
	Utilization Num `json:"utilization"`
}

// VolumeIO returns the I/O of the volume at path, like "/volume1". A nil
// Utilization has none.
func (u *Utilization) VolumeIO(path string) (IOStat, bool) {
	if u == nil {
		return IOStat{}, false
	}
	for _, v := range u.Space.Volume {
		if "/"+v.DisplayName == path {
			return v, true
		}
	}
	return IOStat{}, false
}

// DiskIO returns the I/O of the disk whose Disk.ID is id, like "sata1".
func (u *Utilization) DiskIO(id string) (IOStat, bool) {
	if u == nil {
		return IOStat{}, false
	}
	for _, d := range u.Disk.Disk {
		if d.Device == id {
			return d, true
		}
	}
	return IOStat{}, false
}

func (c *Client) Utilization(ctx context.Context) (*Utilization, error) {
	var out Utilization
	if err := c.Call(ctx, "SYNO.Core.System.Utilization", 1, "get", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
