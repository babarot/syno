package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/babarot/syno/internal/dsm"
)

func newContainerCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "container",
		Short: "Inspect the containers of Container Manager",
	}
	c.AddCommand(newContainerListCmd())
	return c
}

func newContainerListCmd() *cobra.Command {
	var (
		asJSON      bool
		runningOnly bool
		project     string
	)

	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the containers of Container Manager",
		Long: `List the containers of Container Manager with their state and health.

Stopped containers are listed too, unlike docker ps, so that a container that
went down after a deploy is not missed. Environment variables are never read
or printed; use syno api SYNO.Docker.Container list for the raw response.`,
		Example: `  syno container list
  syno container list --running
  syno container list --project web --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := listContainers(cmd.Context(), runningOnly, project)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(list)
			}
			return printContainers(os.Stdout, list.Containers)
		},
	}

	c.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	c.Flags().BoolVar(&runningOnly, "running", false, "List only running containers")
	c.Flags().StringVar(&project, "project", "", "List only the containers of this Docker Compose project")

	return c
}

// containerList is the JSON of syno container list. The host tells which
// NAS answered, since the profile may change between calls.
type containerList struct {
	Host       string         `json:"host"`
	Containers []containerRow `json:"containers"`
}

// listContainers lists the containers as syno container list shows them.
func listContainers(ctx context.Context, runningOnly bool, project string) (*containerList, error) {
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
	return &containerList{Host: client.Base, Containers: containerRows(cs, runningOnly, project)}, nil
}

// containerRow is what syno shows of a container, in the table and in JSON.
type containerRow struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Health  string `json:"health,omitempty"`
	Project string `json:"project,omitempty"`
	Image   string `json:"image"`
	Status  string `json:"status"`
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

func printContainers(out io.Writer, rows []containerRow) error {
	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tHEALTH\tPROJECT\tIMAGE\tSTATUS")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Name, r.State, orDash(r.Health), orDash(r.Project), r.Image, r.Status)
	}
	return w.Flush()
}
