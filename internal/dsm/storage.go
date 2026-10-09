package dsm

import (
	"context"
	"encoding/json"
)

// Storage is SYNO.Storage.CGI.Storage load_info. It needs an administrator.
type Storage struct {
	Disks        []Disk        `json:"disks"`
	StoragePools []StoragePool `json:"storagePools"`
	Volumes      []Volume      `json:"volumes"`
}

// Summary statuses DSM puts on pools and volumes. Unknown values are
// possible on other DSM versions, so callers should not assume this list
// is complete.
const (
	SummaryNormal    = "normal"
	SummaryAttention = "attention"
	SummaryDanger    = "danger"
)

type Disk struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Device      string `json:"device"`
	Vendor      string `json:"vendor"`
	Model       string `json:"model"`
	Serial      string `json:"serial"`
	Firmware    string `json:"firm"`
	DiskType    string `json:"diskType"`
	IsSSD       bool   `json:"isSsd"`
	Status      string `json:"status"`
	SmartStatus string `json:"smart_status"`
	Temp        Num    `json:"temp"`
	SizeTotal   Num    `json:"size_total"` // bytes
	UsedBy      string `json:"used_by"`
	// Unc is the number of uncorrectable sectors.
	Unc Num `json:"unc"`
	// Life estimates DSM computes, mostly for SSDs.
	RemainLifeDanger   bool `json:"remain_life_danger"`
	BelowRemainLifeThr bool `json:"below_remain_life_thr"`
	SBDaysLeftWarning  bool `json:"sb_days_left_warning"`
	SBDaysLeftCritical bool `json:"sb_days_left_critical"`
}

type StoragePool struct {
	ID            string   `json:"id"` // e.g. "reuse_1"
	NumID         int      `json:"num_id"`
	Status        string   `json:"status"`
	SummaryStatus string   `json:"summary_status"`
	DeviceType    string   `json:"device_type"` // e.g. "shr_with_1_disk_protect", "raid_5"
	Disks         []string `json:"disks"`
	Size          struct {
		Total Num `json:"total"`
		Used  Num `json:"used"`
	} `json:"size"`
	SpaceStatus       SpaceStatus       `json:"space_status"`
	DiskFailureNumber int               `json:"disk_failure_number"`
	MissingDrives     []json.RawMessage `json:"missing_drives"`
	// LastDoneTime is the Unix time of the last data scrubbing, 0 if never.
	LastDoneTime int64 `json:"last_done_time"`
	// IsScheduled reports whether data scrubbing runs on a schedule.
	IsScheduled bool `json:"is_scheduled"`
}

type Volume struct {
	ID            string `json:"id"`
	VolPath       string `json:"vol_path"`
	Status        string `json:"status"`
	SummaryStatus string `json:"summary_status"`
	FSType        string `json:"fs_type"`
	PoolPath      string `json:"pool_path"`
	DeviceType    string `json:"device_type"`
	Size          struct {
		Total Num `json:"total"`
		Used  Num `json:"used"`
	} `json:"size"`
	SpaceStatus SpaceStatus `json:"space_status"`
}

// SpaceStatus explains the status of a pool or volume.
type SpaceStatus struct {
	Status string `json:"status"` // e.g. "pool_normal"
	// Detail is why the space is not normal, e.g. "fs_almost_full".
	Detail        string `json:"detail"`
	SummaryStatus string `json:"summary_status"`
}

func (c *Client) Storage(ctx context.Context) (*Storage, error) {
	var out Storage
	if err := c.Call(ctx, "SYNO.Storage.CGI.Storage", 1, "load_info", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
