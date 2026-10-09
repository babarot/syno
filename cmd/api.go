package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/babarot/syno/internal/dsm"
)

func newAPICmd() *cobra.Command {
	var (
		host    string
		version int
		fields  []string
		list    bool
		asJSON  bool
	)

	c := &cobra.Command{
		Use:   "api <api> <method>",
		Short: "Call any DSM Web API and print its data as JSON",
		Long: `Call any DSM Web API with the account saved by "syno login" and print the
"data" field of the response as JSON.

The path and the latest version of the API are looked up with SYNO.API.Info.
For APIs whose request format is JSON, field values that are not valid JSON
are sent as JSON strings, so -f name=foo works as well as -f 'name="foo"'.
Values that are valid JSON are sent as they are: quote a number that the API
expects as a string, as in -f 'id="123"'.

With --list, print the APIs the NAS provides. This needs no login.`,
		Example: `  syno api SYNO.Core.System info
  syno api SYNO.Storage.CGI.Storage load_info -v 1
  syno api SYNO.Core.Share list -f additional='["share_quota"]'
  syno api --list storage`,
		Args: func(cmd *cobra.Command, args []string) error {
			if list {
				return cobra.MaximumNArgs(1)(cmd, args)
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if list {
				client, err := listClient(ctx, host)
				if err != nil {
					return err
				}
				infos, err := client.APIInfo(ctx, "all")
				if err != nil {
					return err
				}
				filter := ""
				if len(args) == 1 {
					filter = args[0]
				}
				return printAPIList(infos, filter, asJSON)
			}

			if host != "" {
				return errors.New("--host only works with --list, other calls use the profile")
			}
			api, method := args[0], args[1]
			params, err := parseFields(fields)
			if err != nil {
				return err
			}

			client, err := connect(ctx)
			if err != nil {
				return err
			}
			defer logout(ctx, client)

			infos, err := client.APIInfo(ctx, api)
			if err != nil {
				return err
			}
			info, ok := infos[api]
			if !ok {
				return fmt.Errorf("unknown API %q, see `syno api --list`", api)
			}
			if version == 0 {
				version = info.MaxVersion
			}
			if info.RequestFormat == "JSON" {
				jsonEncodeValues(params)
			}

			data, err := client.Raw(ctx, info.Path, api, version, method, params)
			if err != nil {
				return err
			}
			return printJSON(data)
		},
	}

	c.Flags().StringVar(&host, "host", "", "With --list, DSM URL to ask (default: the profile, then discover)")
	c.Flags().IntVarP(&version, "version", "v", 0, "API version (default: the latest the NAS supports)")
	c.Flags().StringArrayVarP(&fields, "field", "f", nil, "Add a parameter in key=value form (repeatable)")
	c.Flags().BoolVar(&list, "list", false, "List the APIs the NAS provides, optionally filtered by a substring")
	c.Flags().BoolVar(&asJSON, "json", false, "With --list, output as JSON")

	return c
}

func parseFields(fields []string) (url.Values, error) {
	params := url.Values{}
	for _, f := range fields {
		k, v, ok := strings.Cut(f, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid field %q, want key=value", f)
		}
		params.Add(k, v)
	}
	return params, nil
}

// jsonEncodeValues quotes values that are not already valid JSON.
func jsonEncodeValues(params url.Values) {
	for k, vs := range params {
		for i, v := range vs {
			if !json.Valid([]byte(v)) {
				b, _ := json.Marshal(v)
				vs[i] = string(b)
			}
		}
		params[k] = vs
	}
}

func printAPIList(infos map[string]dsm.APIInfo, filter string, asJSON bool) error {
	names := make([]string, 0, len(infos))
	for name := range infos {
		if strings.Contains(strings.ToLower(name), strings.ToLower(filter)) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	if asJSON {
		out := make(map[string]dsm.APIInfo, len(names))
		for _, name := range names {
			out[name] = infos[name]
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "API\tVERSIONS\tPATH\tFORMAT")
	for _, name := range names {
		i := infos[name]
		fmt.Fprintf(w, "%s\t%d-%d\t%s\t%s\n", name, i.MinVersion, i.MaxVersion, i.Path, orDash(i.RequestFormat))
	}
	return w.Flush()
}

func printJSON(data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		return err
	}
	buf.WriteByte('\n')
	_, err := buf.WriteTo(os.Stdout)
	return err
}

// listClient picks the DSM to ask for --list, which needs no login:
// --host, then the selected profile, then discovery. Only the profile comes
// with a way to trust its certificate; the others are not verified, which
// is fine because the call sends no credentials.
func listClient(ctx context.Context, host string) (*dsm.Client, error) {
	if host != "" {
		return dsm.NewInsecure(host), nil
	}
	if _, p, err := selectProfile(); err == nil {
		return dsm.New(p.URL, p.TLS.Pin), nil
	}
	u, _, err := discoverOne(ctx)
	if err != nil {
		return nil, err
	}
	return dsm.NewInsecure(u), nil
}
