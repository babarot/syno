package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/babarot/syno/internal/dsm"
)

func newStatusCmd() *cobra.Command {
	var (
		raw    bool
		asJSON bool
	)

	c := &cobra.Command{
		Use:   "status",
		Short: "Show system, utilization, volumes and disks of the NAS",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if raw {
				client, release, err := connect(ctx)
				if err != nil {
					return err
				}
				defer release()
				return printRaw(cmd, client)
			}

			s, err := loadStatus(ctx)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(s.report())
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			printSystem(w, s.host, s.sys, s.util)
			fmt.Fprintln(w)
			printPools(w, s.st)
			fmt.Fprintln(w)
			printVolumes(w, s.st)
			fmt.Fprintln(w)
			printDisks(w, s.st, s.powerOn)
			return w.Flush()
		},
	}

	c.Flags().BoolVar(&raw, "raw", false, "Print the raw API responses as JSON")
	c.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	c.MarkFlagsMutuallyExclusive("raw", "json")

	return c
}

// status is what syno status shows, as DSM returns it.
type status struct {
	host string
	sys  *dsm.SystemInfo
	util *dsm.Utilization
	st   *dsm.Storage
	// powerOn is the power-on hours of each disk by Disk.ID, for the disks
	// whose SMART overview could be read.
	powerOn map[string]float64
}

func loadStatus(ctx context.Context) (*status, error) {
	client, release, err := connect(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return readStatus(ctx, client)
}

// readStatus is loadStatus with a client that is already logged in.
func readStatus(ctx context.Context, client *dsm.Client) (*status, error) {
	s := &status{host: client.Base}
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) { s.sys, err = client.SystemInfo(gctx); return })
	g.Go(func() (err error) { s.util, err = client.Utilization(gctx); return })
	g.Go(func() (err error) {
		if s.st, err = client.Storage(gctx); err != nil {
			return err
		}
		// One call per disk, in the time Utilization takes anyway.
		s.powerOn = powerOnHours(gctx, client, s.st.Disks)
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return s, nil
}

// diskHealthSource is the part of dsm.Client that powerOnHours uses.
type diskHealthSource interface {
	DiskHealth(ctx context.Context, device string) (*dsm.DiskHealth, error)
}

// powerOnHours reads the power-on hours of the disks in parallel. A disk
// whose SMART overview cannot be read is left out rather than failing the
// whole status, since the hours are a detail.
func powerOnHours(ctx context.Context, c diskHealthSource, disks []dsm.Disk) map[string]float64 {
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = map[string]float64{}
	)
	for _, d := range disks {
		if d.Device == "" {
			continue
		}
		wg.Go(func() {
			h, err := c.DiskHealth(ctx, d.Device)
			if err != nil || h.PowerOnHours <= 0 {
				return
			}
			mu.Lock()
			out[d.ID] = float64(h.PowerOnHours)
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}

// statusReport is the JSON of syno status --json and the syno_status MCP
// tool: the table's contents with sizes in bytes.
type statusReport struct {
	Host          string         `json:"host"`
	Model         string         `json:"model"`
	Serial        string         `json:"serial"`
	DSM           string         `json:"dsm"`
	Uptime        string         `json:"uptime"`
	TemperatureC  float64        `json:"temperature_c"`
	CPUPercent    float64        `json:"cpu_percent"`
	MemoryPercent float64        `json:"memory_percent"`
	MemoryBytes   float64        `json:"memory_bytes"`
	Pools         []poolReport   `json:"pools"`
	Volumes       []volumeReport `json:"volumes"`
	Disks         []diskReport   `json:"disks"`
}

type poolReport struct {
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	Type       string  `json:"type"`
	Disks      int     `json:"disks"`
	UsedBytes  float64 `json:"used_bytes"`
	TotalBytes float64 `json:"total_bytes"`
}

type volumeReport struct {
	Path       string  `json:"path"`
	Status     string  `json:"status"`
	Detail     string  `json:"detail,omitempty"`
	FS         string  `json:"fs"`
	Pool       string  `json:"pool"`
	UsedBytes  float64 `json:"used_bytes"`
	TotalBytes float64 `json:"total_bytes"`
}

type diskReport struct {
	Name         string  `json:"name"`
	Model        string  `json:"model"`
	SizeBytes    float64 `json:"size_bytes"`
	Status       string  `json:"status"`
	SMART        string  `json:"smart"`
	TemperatureC float64 `json:"temperature_c"`
	// LifePercent is the life left that DSM estimates, mostly for SSDs.
	LifePercent  *float64 `json:"life_percent,omitempty"`
	PowerOnHours *float64 `json:"power_on_hours,omitempty"`
	Pool         string   `json:"pool"`
}

func (s *status) report() statusReport {
	u := s.util
	r := statusReport{
		Host:          s.host,
		Model:         s.sys.Model,
		Serial:        s.sys.Serial,
		DSM:           s.sys.FirmwareVer,
		Uptime:        formatUptime(s.sys.UpTime),
		TemperatureC:  float64(s.sys.SysTemp),
		CPUPercent:    float64(u.CPU.UserLoad + u.CPU.SystemLoad + u.CPU.OtherLoad),
		MemoryPercent: float64(u.Memory.RealUsage),
		MemoryBytes:   float64(u.Memory.TotalReal) * 1024,
	}
	r.Pools, r.Volumes, r.Disks = storageReports(s.st, s.powerOn)
	return r
}

// storageReports turns the storage of status into its JSON.
func storageReports(st *dsm.Storage, powerOn map[string]float64) ([]poolReport, []volumeReport, []diskReport) {
	pools, volumes, disks := []poolReport{}, []volumeReport{}, []diskReport{}
	for _, p := range st.StoragePools {
		pools = append(pools, poolReport{
			Name: poolName(st, p.ID), Status: p.Status, Type: p.DeviceType, Disks: len(p.Disks),
			UsedBytes: float64(p.Size.Used), TotalBytes: float64(p.Size.Total),
		})
	}
	for _, v := range st.Volumes {
		volumes = append(volumes, volumeReport{
			Path: v.VolPath, Status: v.Status, Detail: v.SpaceStatus.Detail, FS: v.FSType, Pool: poolName(st, v.PoolPath),
			UsedBytes: float64(v.Size.Used), TotalBytes: float64(v.Size.Total),
		})
	}
	for _, d := range st.Disks {
		dr := diskReport{
			Name: d.Name, Model: strings.Join(strings.Fields(d.Vendor+" "+d.Model), " "), SizeBytes: float64(d.SizeTotal),
			Status: d.Status, SMART: d.SmartStatus, TemperatureC: float64(d.Temp), Pool: poolName(st, d.UsedBy),
		}
		if v, ok := d.LifePercent(); ok {
			dr.LifePercent = &v
		}
		if h, ok := powerOn[d.ID]; ok {
			dr.PowerOnHours = &h
		}
		disks = append(disks, dr)
	}
	return pools, volumes, disks
}

func printSystem(w io.Writer, host string, s *dsm.SystemInfo, u *dsm.Utilization) {
	cpu := u.CPU.UserLoad + u.CPU.SystemLoad + u.CPU.OtherLoad
	fmt.Fprintln(w, "SYSTEM")
	fmt.Fprintf(w, "  Host\t%s\n", host)
	fmt.Fprintf(w, "  Model\t%s\n", s.Model)
	fmt.Fprintf(w, "  Serial\t%s\n", s.Serial)
	fmt.Fprintf(w, "  DSM\t%s\n", s.FirmwareVer)
	fmt.Fprintf(w, "  Uptime\t%s\n", formatUptime(s.UpTime))
	fmt.Fprintf(w, "  Temperature\t%.0f°C\n", float64(s.SysTemp))
	fmt.Fprintf(w, "  CPU\t%.0f%%\n", float64(cpu))
	fmt.Fprintf(w, "  Memory\t%.0f%% of %s\n", float64(u.Memory.RealUsage), humanBytes(float64(u.Memory.TotalReal)*1024))
}

func printPools(w io.Writer, st *dsm.Storage) {
	fmt.Fprintln(w, "POOL\tSTATUS\tTYPE\tDISKS\tUSED\tTOTAL\tUSE%")
	for _, p := range st.StoragePools {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			poolName(st, p.ID), p.Status, p.DeviceType, len(p.Disks),
			humanBytes(float64(p.Size.Used)), humanBytes(float64(p.Size.Total)),
			percent(p.Size.Used, p.Size.Total))
	}
}

func printVolumes(w io.Writer, st *dsm.Storage) {
	fmt.Fprintln(w, "VOLUME\tSTATUS\tFS\tPOOL\tUSED\tTOTAL\tUSE%")
	for _, v := range st.Volumes {
		status := v.Status
		if status != "normal" && v.SpaceStatus.Detail != "" {
			status += " (" + v.SpaceStatus.Detail + ")"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			v.VolPath, status, v.FSType, poolName(st, v.PoolPath),
			humanBytes(float64(v.Size.Used)), humanBytes(float64(v.Size.Total)),
			percent(v.Size.Used, v.Size.Total))
	}
}

func printDisks(w io.Writer, st *dsm.Storage, powerOn map[string]float64) {
	fmt.Fprintln(w, "DISK\tMODEL\tSIZE\tSTATUS\tSMART\tTEMP\tLIFE\tPOWER-ON\tPOOL")
	for _, d := range st.Disks {
		life := "-"
		if v, ok := d.LifePercent(); ok {
			life = fmt.Sprintf("%.0f%%", v)
		}
		hours := "-"
		if h, ok := powerOn[d.ID]; ok {
			hours = formatHours(h)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%.0f°C\t%s\t%s\t%s\n",
			d.Name, strings.Join(strings.Fields(d.Vendor+" "+d.Model), " "),
			humanBytes(float64(d.SizeTotal)), d.Status, d.SmartStatus,
			float64(d.Temp), life, hours, poolName(st, d.UsedBy))
	}
}

// formatHours shortens power-on hours to the largest unit that fits:
// "18h", "40d", "2.6y".
func formatHours(h float64) string {
	switch {
	case h < 48:
		return fmt.Sprintf("%.0fh", h)
	case h < 365*24:
		return fmt.Sprintf("%.0fd", h/24)
	default:
		return fmt.Sprintf("%.1fy", h/(365*24))
	}
}

// poolName turns an internal pool ID like "reuse_1" into "Pool 1" as DSM shows it.
func poolName(st *dsm.Storage, id string) string {
	for _, p := range st.StoragePools {
		if p.ID == id {
			return fmt.Sprintf("Pool %d", p.NumID)
		}
	}
	if id == "" {
		return "-"
	}
	return id
}

// printRaw dumps the API responses as they are, to check field names
// against what this DSM version actually returns.
func printRaw(cmd *cobra.Command, c *dsm.Client) error {
	ctx := cmd.Context()
	out := map[string]json.RawMessage{}
	for _, call := range []struct {
		api, method string
		version     int
	}{
		{"SYNO.Core.System", "info", 1},
		{"SYNO.Core.System.Utilization", "get", 1},
		{"SYNO.Storage.CGI.Storage", "load_info", 1},
	} {
		data, err := c.Raw(ctx, "entry.cgi", call.api, call.version, call.method, nil)
		if err != nil {
			out[call.api] = json.RawMessage(strconv.Quote(err.Error()))
			continue
		}
		out[call.api] = data
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// formatUptime turns DSM's "h:m:s" (hours can exceed 24) into "12d 3h 4m".
func formatUptime(s string) string {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return s
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return s
	}
	return fmt.Sprintf("%dd %dh %dm", h/24, h%24, m)
}

func humanBytes(b float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for b >= 1024 && i < len(units)-1 {
		b /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", b, units[i])
	}
	return fmt.Sprintf("%.1f %s", b, units[i])
}

func percent(used, total dsm.Num) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", float64(used)/float64(total)*100)
}
