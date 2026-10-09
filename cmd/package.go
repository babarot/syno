package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/babarot/syno/internal/dsm"
)

func newPackageCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "package",
		Short: "Inspect the installed packages",
	}
	c.AddCommand(newPackageListCmd())
	return c
}

func newPackageListCmd() *cobra.Command {
	var (
		asJSON   bool
		outdated bool
	)

	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the installed packages and the updates available",
		Long: `List the installed packages with the latest version in Package Center.

LATEST shows the newer version when there is one, marked "(security)" for a
security update, "-" when the package is up to date, and "?" when Package
Center does not know the package, as with third-party ones. Nothing is
updated.`,
		Example: `  syno package list
  syno package list --outdated
  syno package list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, err := connect(ctx)
			if err != nil {
				return err
			}
			defer logout(ctx, client)

			var (
				installed []dsm.Package
				store     []dsm.StorePackage
			)
			g, gctx := errgroup.WithContext(ctx)
			g.Go(func() (err error) { installed, err = client.Packages(gctx); return })
			g.Go(func() (err error) { store, err = client.StorePackages(gctx); return })
			if err := g.Wait(); err != nil {
				return err
			}

			rows := packageRows(dsm.PackageUpdates(installed, store), outdated)
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			return printPackages(os.Stdout, rows)
		},
	}

	c.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	c.Flags().BoolVar(&outdated, "outdated", false, "List only the packages with an update")

	return c
}

// packageRow is what syno shows of a package, in the table and in JSON.
type packageRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	Latest   string `json:"latest,omitempty"`
	Security bool   `json:"security"`
	InStore  bool   `json:"in_store"`
	Status   string `json:"status"`
}

func packageRows(us []dsm.PackageUpdate, outdated bool) []packageRow {
	rows := make([]packageRow, 0, len(us))
	for _, u := range us {
		if outdated && !u.Available() {
			continue
		}
		rows = append(rows, packageRow{
			ID:       u.ID,
			Name:     u.Name,
			Version:  u.Version,
			Latest:   u.Latest,
			Security: u.Security,
			InStore:  u.InStore,
			Status:   u.Additional.Status,
		})
	}
	return rows
}

func printPackages(out io.Writer, rows []packageRow) error {
	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tVERSION\tLATEST\tSTATUS")
	for _, r := range rows {
		latest := r.Latest
		switch {
		case !r.InStore:
			latest = "?"
		case latest == "":
			latest = "-"
		case r.Security:
			latest += " (security)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.ID, r.Name, r.Version, latest, orDash(r.Status))
	}
	return w.Flush()
}
