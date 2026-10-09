package cmd

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

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
	var (
		asJSON  bool
		recycle bool
	)

	c := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the shared folders by the space they use",
		Long: `List the shared folders with the space each uses, the largest first, to see
what fills a volume. FLAGS shows hidden, encrypted, read-only, usb and
recycle-bin where they apply.

--recycle also sums what each recycle bin holds, the space emptying it would
free. The NAS sums the files one by one, so a recycle bin with many files
takes a while; it gives up after 2 minutes.`,
		Example: `  syno share list
  syno share list --recycle
  syno share list --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := listShares(cmd.Context(), recycle)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(list)
			}
			return printShares(os.Stdout, list.Shares, recycle)
		},
	}

	c.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	c.Flags().BoolVar(&recycle, "recycle", false, "Also sum what each recycle bin holds")

	return c
}

// shareList is the JSON of syno share list --json and the syno_shares MCP
// tool.
type shareList struct {
	Host   string     `json:"host"`
	Shares []shareRow `json:"shares"`
}

// listShares lists the shared folders as syno share list shows them, with
// the size of their recycle bins when recycle is set.
func listShares(ctx context.Context, recycle bool) (*shareList, error) {
	client, release, err := connect(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return readShares(ctx, client, recycle)
}

// readShares is listShares with a client that is already logged in.
func readShares(ctx context.Context, client *dsm.Client, recycle bool) (*shareList, error) {
	ss, err := client.Shares(ctx)
	if err != nil {
		return nil, err
	}
	rows := shareRows(ss)
	if recycle {
		if err := measureRecycleBins(ctx, client, rows); err != nil {
			return nil, err
		}
	}
	return &shareList{Host: client.Base, Shares: rows}, nil
}

// recycleTimeout bounds summing the recycle bins. It takes about a second
// per 5,000 files.
const recycleTimeout = 2 * time.Minute

// dirMeasurer is the part of dsm.Client that measureRecycleBins uses.
type dirMeasurer interface {
	MeasureDir(ctx context.Context, path string, interval time.Duration) (*dsm.DirSize, error)
}

// measureRecycleBins fills RecycleBinBytes of the shares whose recycle bin
// is on. DirSize sums a missing folder to 0 without an error, so shares
// without a recycle bin are not asked about.
func measureRecycleBins(ctx context.Context, c dirMeasurer, rows []shareRow) error {
	ctx, cancel := context.WithTimeout(ctx, recycleTimeout)
	defer cancel()

	var (
		mu         sync.Mutex
		unfinished []string
	)
	g, gctx := errgroup.WithContext(ctx)
	for i := range rows {
		r := &rows[i]
		if !r.RecycleBin {
			continue
		}
		g.Go(func() error {
			s, err := c.MeasureDir(gctx, "/"+r.Name+"/#recycle", 500*time.Millisecond)
			if errors.Is(err, context.DeadlineExceeded) && s != nil {
				mu.Lock()
				unfinished = append(unfinished, fmt.Sprintf("%s (%.0f files so far)", r.Name, float64(s.NumFile)))
				mu.Unlock()
				return nil
			}
			if err != nil {
				return fmt.Errorf("recycle bin of %s: %w", r.Name, err)
			}
			size := float64(s.TotalSize)
			r.RecycleBinBytes = &size
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	if len(unfinished) > 0 {
		slices.Sort(unfinished)
		return fmt.Errorf("gave up summing the recycle bins after %v: %s", recycleTimeout, strings.Join(unfinished, ", "))
	}
	return nil
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
	// RecycleBinBytes is what the recycle bin holds, set only when asked
	// for and the recycle bin is on.
	RecycleBinBytes *float64 `json:"recycle_bin_bytes,omitempty"`
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

func printShares(out io.Writer, rows []shareRow, recycle bool) error {
	w := tabwriter.NewWriter(out, 0, 0, 3, ' ', 0)
	if recycle {
		fmt.Fprintln(w, "NAME\tVOLUME\tUSED\tRECYCLE BIN\tFLAGS")
	} else {
		fmt.Fprintln(w, "NAME\tVOLUME\tUSED\tFLAGS")
	}
	for _, r := range rows {
		if recycle {
			bin := "-"
			if r.RecycleBinBytes != nil {
				bin = humanBytes(*r.RecycleBinBytes)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Name, r.Volume, humanBytes(r.UsedBytes), bin, r.flags())
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name, r.Volume, humanBytes(r.UsedBytes), r.flags())
	}
	return w.Flush()
}
