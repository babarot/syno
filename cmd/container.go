package cmd

import (
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
			ctx := cmd.Context()
			client, err := connect(ctx)
			if err != nil {
				return err
			}
			defer logout(ctx, client)

			cs, err := client.Containers(ctx)
			if dsm.IsNoAPI(err, dsm.ContainerAPI) {
				return errors.New("the NAS does not have Container Manager installed")
			}
			if err != nil {
				return err
			}

			rows := containerRows(cs, runningOnly, project)
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			return printContainers(os.Stdout, rows)
		},
	}

	c.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	c.Flags().BoolVar(&runningOnly, "running", false, "List only running containers")
	c.Flags().StringVar(&project, "project", "", "List only the containers of this Docker Compose project")

	return c
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
