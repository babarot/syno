package cmd

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/babarot/syno/internal/dsm"
)

func newShareCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "share",
		Short: "Inspect the shared folders",
	}
	c.AddCommand(newShareListCmd())
	return c
}

func newShareListCmd() *cobra.Command {
	var asJSON bool

	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the shared folders by the space they use",
		Long: `List the shared folders with the space each uses, the largest first, to see
what fills a volume. FLAGS shows hidden, encrypted, read-only, usb and
recycle-bin where they apply.`,
		Example: `  syno share list
  syno share list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := listShares(cmd.Context())
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(list)
			}
			return printShares(os.Stdout, list.Shares)
		},
	}

	c.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return c
}

// shareList is the JSON of syno share list --json and the syno_shares MCP
// tool.
type shareList struct {
	Host   string     `json:"host"`
	Shares []shareRow `json:"shares"`
}

// listShares lists the shared folders as syno share list shows them.
func listShares(ctx context.Context) (*shareList, error) {
	client, release, err := connect(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	ss, err := client.Shares(ctx)
	if err != nil {
		return nil, err
	}
	return &shareList{Host: client.Base, Shares: shareRows(ss)}, nil
}

// shareRow is what syno shows of a shared folder, in the table and in JSON.
type shareRow struct {
	Name        string  `json:"name"`
	Volume      string  `json:"volume"`
	Description string  `json:"description,omitempty"`
	UsedBytes   float64 `json:"used_bytes"`
	Hidden      bool    `json:"hidden"`
	Encrypted   bool    `json:"encrypted"`
	ReadOnly    bool    `json:"read_only"`
	USB         bool    `json:"usb"`
	RecycleBin  bool    `json:"recycle_bin"`
}

const mib = 1024 * 1024

// shareRows converts the shares and sorts them by the space they use, the
// largest first.
func shareRows(ss []dsm.Share) []shareRow {
	rows := make([]shareRow, 0, len(ss))
	for _, s := range ss {
		rows = append(rows, shareRow{
			Name:        s.Name,
			Volume:      s.VolPath,
			Description: s.Desc,
			UsedBytes:   float64(s.QuotaUsed) * mib,
			Hidden:      s.Hidden,
			Encrypted:   s.Encryption != 0,
			ReadOnly:    s.IsForceReadonly,
			USB:         s.IsUSBShare,
			RecycleBin:  s.RecycleBin,
		})
	}
	slices.SortStableFunc(rows, func(a, b shareRow) int {
		if c := cmp.Compare(b.UsedBytes, a.UsedBytes); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return rows
}

func (r shareRow) flags() string {
	var fs []string
	for _, f := range []struct {
		on   bool
		name string
	}{
		{r.Hidden, "hidden"},
		{r.Encrypted, "encrypted"},
		{r.ReadOnly, "read-only"},
		{r.USB, "usb"},
		{r.RecycleBin, "recycle-bin"},
	} {
		if f.on {
			fs = append(fs, f.name)
		}
	}
	return strings.Join(fs, ",")
}

func printShares(out io.Writer, rows []shareRow) error {
	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tVOLUME\tUSED\tFLAGS")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name, r.Volume, humanBytes(r.UsedBytes), r.flags())
	}
	return w.Flush()
}
