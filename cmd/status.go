package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/babarot/syno/internal/dsm"
)

func newStatusCmd() *cobra.Command {
	var raw bool

	c := &cobra.Command{
		Use:   "status",
		Short: "Show system, utilization, volumes and disks of the NAS",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, err := connect(ctx)
			if err != nil {
				return err
			}
			defer logout(ctx, client)

			if raw {
				return printRaw(cmd, client)
			}

			var (
				sys  *dsm.SystemInfo
				util *dsm.Utilization
				st   *dsm.Storage
			)
			g, gctx := errgroup.WithContext(ctx)
			g.Go(func() (err error) { sys, err = client.SystemInfo(gctx); return })
			g.Go(func() (err error) { util, err = client.Utilization(gctx); return })
			g.Go(func() (err error) { st, err = client.Storage(gctx); return })
			if err := g.Wait(); err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
			printSystem(w, client.Base, sys, util)
			fmt.Fprintln(w)
			printPools(w, st)
			fmt.Fprintln(w)
			printVolumes(w, st)
			fmt.Fprintln(w)
			printDisks(w, st)
			return w.Flush()
		},
	}

	c.Flags().BoolVar(&raw, "raw", false, "Print the raw API responses as JSON")

	return c
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

func printDisks(w io.Writer, st *dsm.Storage) {
	fmt.Fprintln(w, "DISK\tMODEL\tSIZE\tSTATUS\tSMART\tTEMP\tPOOL")
	for _, d := range st.Disks {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%.0f°C\t%s\n",
			d.Name, strings.Join(strings.Fields(d.Vendor+" "+d.Model), " "),
			humanBytes(float64(d.SizeTotal)), d.Status, d.SmartStatus,
			float64(d.Temp), poolName(st, d.UsedBy))
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
