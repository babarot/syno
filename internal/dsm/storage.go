package dsm

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"
)

// Storage is SYNO.Storage.CGI.Storage load_info. It needs an administrator.
type Storage struct {
	Disks        []Disk        `json:"disks"`
	StoragePools []StoragePool `json:"storagePools"`
	Volumes      []Volume      `json:"volumes"`
	Env          struct {
		// BayNumber is the number of drive bays of the NAS itself, without
		// M.2 slots and expansion units.
		BayNumber Num `json:"bay_number"`
	} `json:"env"`
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
	// SlotID is the bay the disk is in, counted from 1 in its enclosure.
	SlotID    int `json:"slot_id"`
	Container struct {
		// Type is "internal" for the NAS itself, and else an expansion unit.
		Type string `json:"type"`
	} `json:"container"`
	// Unc is the number of uncorrectable sectors.
	Unc Num `json:"unc"`
	// RemainLife is the life left in percent that DSM estimates, mostly for
	// SSDs. Value is -1 when DSM has no estimate, as for most HDDs, and
	// RemainLife is nil on a DSM that does not send it.
	RemainLife *struct {
		Value Num `json:"value"`
	} `json:"remain_life"`
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

// LifePercent returns the life left in percent that DSM estimates, and false
// when it has none.
func (d Disk) LifePercent() (float64, bool) {
	if d.RemainLife == nil {
		return 0, false
	}
	v := float64(d.RemainLife.Value)
	return v, v >= 0
}

// DiskHealth is the part of SYNO.Storage.CGI.Smart get_health_info that
// load_info lacks.
type DiskHealth struct {
	PowerOnHours Num `json:"poweron"`
}

// DiskHealth reads the SMART overview of one disk. device is Disk.Device,
// like "/dev/sata1". It takes one call per disk.
func (c *Client) DiskHealth(ctx context.Context, device string) (*DiskHealth, error) {
	b, err := json.Marshal(device)
	if err != nil {
		return nil, err
	}
	var out struct {
		HealthInfo struct {
			Overview DiskHealth `json:"overview"`
		} `json:"healthInfo"`
	}
	if err := c.Call(ctx, "SYNO.Storage.CGI.Smart", 1, "get_health_info", url.Values{"device": {string(b)}}, &out); err != nil {
		return nil, err
	}
	return &out.HealthInfo.Overview, nil
}

// Bays tells how many drive bays the NAS itself has and which of them are
// empty. ok is false when DSM does not say how many it has. M.2 SSDs and the
// disks of expansion units are not in the bays.
func (s *Storage) Bays() (total int, empty []int, ok bool) {
	total = int(s.Env.BayNumber)
	if total <= 0 {
		return 0, nil, false
	}
	used := map[int]bool{}
	for _, d := range s.Disks {
		if d.Container.Type == "internal" && !d.IsM2() {
			used[d.SlotID] = true
		}
	}
	for slot := 1; slot <= total; slot++ {
		if !used[slot] {
			empty = append(empty, slot)
		}
	}
	return total, empty, true
}

// IsM2 reports whether the disk is an NVMe SSD in an M.2 slot, which DSM
// names like "nvme0n1".
func (d Disk) IsM2() bool {
	return slices.ContainsFunc([]string{d.ID, d.Device}, func(s string) bool { return strings.Contains(s, "nvme") })
}
