package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/babarot/syno/internal/dsm"
)

func newContainerCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "container",
		Short: "Inspect, start and stop the containers of Container Manager",
	}
	c.AddCommand(
		newContainerListCmd(),
		newContainerActionCmd("start"),
		newContainerActionCmd("stop"),
		newContainerActionCmd("restart"),
	)
	return c
}

func newContainerListCmd() *cobra.Command {
	var (
		asJSON bool
		opts   containerListOptions
	)

	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the containers of Container Manager",
		Long: `List the containers of Container Manager with their state and health.

Stopped containers are listed too, unlike docker ps, so that a container that
went down after a deploy is not missed. Environment variables are never read
or printed; use syno api SYNO.Docker.Container list for the raw response.

--usage adds the CPU and memory each running container uses, as docker stats
shows them: 100% CPU is one core, and memory leaves out the page cache. CPU
needs two samples, so it takes a second or two longer.`,
		Example: `  syno container list
  syno container list --running --usage
  syno container list --project web --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := listContainers(cmd.Context(), opts)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(list)
			}
			return printContainers(os.Stdout, list.Containers, opts.Usage)
		},
	}

	c.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	c.Flags().BoolVar(&opts.RunningOnly, "running", false, "List only running containers")
	c.Flags().StringVar(&opts.Project, "project", "", "List only the containers of this Docker Compose project")
	c.Flags().BoolVar(&opts.Usage, "usage", false, "Add the CPU and memory each running container uses")

	return c
}

// containerList is the JSON of syno container list. The host tells which
// NAS answered, since the profile may change between calls.
type containerList struct {
	Host       string         `json:"host"`
	Containers []containerRow `json:"containers"`
}

// containerListOptions are the flags of syno container list.
type containerListOptions struct {
	RunningOnly bool
	Project     string
	Usage       bool
}

// usageInterval is the time between the two samples CPU usage needs.
var usageInterval = time.Second

// listContainers lists the containers as syno container list shows them.
func listContainers(ctx context.Context, opts containerListOptions) (*containerList, error) {
	client, release, err := connect(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	cs, err := client.Containers(ctx)
	if dsm.IsNoAPI(err, dsm.ContainerAPI) {
		return nil, errors.New("the NAS does not have Container Manager installed")
	}
	if err != nil {
		return nil, err
	}
	rows := containerRows(cs, opts.RunningOnly, opts.Project)
	if opts.Usage {
		if err := addUsage(ctx, client, rows); err != nil {
			return nil, err
		}
	}
	return &containerList{Host: client.Base, Containers: rows}, nil
}

// statsSource is the part of dsm.Client that addUsage uses.
type statsSource interface {
	ContainerStats(ctx context.Context) (map[string]dsm.ContainerStat, error)
}

// addUsage fills the CPU and memory of the running containers from two
// samples of their stats, at least usageInterval apart. A call for the stats
// takes about a second by itself, so there is often nothing left to wait.
func addUsage(ctx context.Context, c statsSource, rows []containerRow) error {
	start := time.Now()
	before, err := c.ContainerStats(ctx)
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Until(start.Add(usageInterval))):
	}
	after, err := c.ContainerStats(ctx)
	if err != nil {
		return err
	}
	for i := range rows {
		r := &rows[i]
		b, ok1 := before[r.Name]
		a, ok2 := after[r.Name]
		if r.State != "running" || !ok1 || !ok2 {
			continue
		}
		if cpu, ok := dsm.CPUPercent(b, a); ok {
			r.CPUPercent = &cpu
		}
		if mem := a.MemoryBytes(); mem > 0 {
			r.MemoryBytes = &mem
		}
	}
	return nil
}

// containerRow is what syno shows of a container, in the table and in JSON.
type containerRow struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Health  string `json:"health,omitempty"`
	Project string `json:"project,omitempty"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	// CPUPercent and MemoryBytes are set with --usage for running
	// containers. 100% CPU is one core.
	CPUPercent  *float64 `json:"cpu_percent,omitempty"`
	MemoryBytes *float64 `json:"memory_bytes,omitempty"`
}

func containerRows(cs []dsm.Container, runningOnly bool, project string) []containerRow {
	rows := make([]containerRow, 0, len(cs))
	for _, c := range cs {
		if runningOnly && !c.State.Running {
			continue
		}
		if project != "" && c.Project() != project {
			continue
		}
		state := c.State.Status
		if state == "" {
			state = c.Status
		}
		rows = append(rows, containerRow{
			Name:    c.Name,
			State:   state,
			Health:  c.Health(),
			Project: c.Project(),
			Image:   c.Image,
			Status:  c.UpStatus,
		})
	}
	return rows
}

func printContainers(out io.Writer, rows []containerRow, usage bool) error {
	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	if usage {
		fmt.Fprintln(w, "NAME\tSTATE\tHEALTH\tPROJECT\tIMAGE\tCPU\tMEM\tSTATUS")
	} else {
		fmt.Fprintln(w, "NAME\tSTATE\tHEALTH\tPROJECT\tIMAGE\tSTATUS")
	}
	for _, r := range rows {
		if usage {
			cpu, mem := "-", "-"
			if r.CPUPercent != nil {
				cpu = fmt.Sprintf("%.1f%%", *r.CPUPercent)
			}
			if r.MemoryBytes != nil {
				mem = humanBytes(*r.MemoryBytes)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Name, r.State, orDash(r.Health), orDash(r.Project), r.Image, cpu, mem, r.Status)
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Name, r.State, orDash(r.Health), orDash(r.Project), r.Image, r.Status)
	}
	return w.Flush()
}
