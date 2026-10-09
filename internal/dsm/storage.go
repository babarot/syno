package dsm

import (
	"context"
)

// Storage is SYNO.Storage.CGI.Storage load_info. It needs an administrator.
type Storage struct {
	Disks        []Disk        `json:"disks"`
	StoragePools []StoragePool `json:"storagePools"`
	Volumes      []Volume      `json:"volumes"`
}

type Disk struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Device      string `json:"device"`
	Vendor      string `json:"vendor"`
	Model       string `json:"model"`
	Serial      string `json:"serial"`
	Firmware    string `json:"firm"`
	DiskType    string `json:"diskType"`
	Status      string `json:"status"`
	SmartStatus string `json:"smart_status"`
	Temp        Num    `json:"temp"`
	SizeTotal   Num    `json:"size_total"` // bytes
	UsedBy      string `json:"used_by"`
}

type StoragePool struct {
	ID         string   `json:"id"` // e.g. "reuse_1"
	NumID      int      `json:"num_id"`
	Status     string   `json:"status"`
	DeviceType string   `json:"device_type"` // e.g. "shr_with_1_disk_protect", "raid_5"
	Disks      []string `json:"disks"`
	Size       struct {
		Total Num `json:"total"`
		Used  Num `json:"used"`
	} `json:"size"`
}

type Volume struct {
	ID         string `json:"id"`
	VolPath    string `json:"vol_path"`
	Status     string `json:"status"`
	FSType     string `json:"fs_type"`
	PoolPath   string `json:"pool_path"`
	DeviceType string `json:"device_type"`
	Size       struct {
		Total Num `json:"total"`
		Used  Num `json:"used"`
	} `json:"size"`
	SpaceStatus struct {
		// Why the volume is not normal, e.g. "fs_almost_full".
		Detail string `json:"detail"`
	} `json:"space_status"`
}

func (c *Client) Storage(ctx context.Context) (*Storage, error) {
	var out Storage
	if err := c.Call(ctx, "SYNO.Storage.CGI.Storage", 1, "load_info", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
