package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/babarot/syno/internal/dsm"
)

// containerVerbs are how the actions read in messages.
var containerVerbs = map[string]struct{ ask, doing string }{
	"start":   {"Start", "Starting"},
	"stop":    {"Stop", "Stopping"},
	"restart": {"Restart", "Restarting"},
}

func newContainerActionCmd(action string) *cobra.Command {
	var (
		yes    bool
		asJSON bool
	)
	verb := containerVerbs[action]

	c := &cobra.Command{
		Use:   action + " NAME...",
		Short: verb.ask + " containers of Container Manager",
		Long: verb.ask + ` containers of Container Manager, one after another in the order given.
syno asks before changing anything; --yes skips the question, as scripts and
AI agents need. Each action takes a few seconds, and DSM cannot cancel one
once it is sent.

Starting a running container or stopping a stopped one does nothing. When a
container fails, syno shows the reason Docker gave and leaves the rest
alone.`,
		Example: fmt.Sprintf("  syno container %[1]s web-app-1\n  syno container %[1]s web-app-1 worker-1 --yes", action),
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, release, err := connect(ctx)
			if err != nil {
				return err
			}
			defer release()

			p := actionPrompt{
				yes:      yes,
				in:       os.Stdin,
				terminal: term.IsTerminal(int(os.Stdin.Fd())),
				out:      os.Stderr,
			}
			results, err := runContainerAction(ctx, client, client.Base, action, args, p)
			if asJSON && results != nil {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if jerr := enc.Encode(containerActionList{Host: client.Base, Results: results}); jerr != nil {
					return jerr
				}
			}
			return err
		},
	}

	c.Flags().BoolVarP(&yes, "yes", "y", false, "Do it without asking")
	c.Flags().BoolVar(&asJSON, "json", false, "Output the results as JSON")

	return c
}

// containerOps is the part of dsm.Client the actions use.
type containerOps interface {
	Containers(ctx context.Context) ([]dsm.Container, error)
	ContainerAction(ctx context.Context, action, name string) (*dsm.ContainerActionResult, error)
}

// actionPrompt is where the question goes and the answer comes from.
type actionPrompt struct {
	yes      bool
	in       io.Reader
	terminal bool // whether in is a terminal someone can answer from
	out      io.Writer
}

type containerActionList struct {
	Host    string                  `json:"host"`
	Results []containerActionResult `json:"results"`
}

type containerActionResult struct {
	Name    string  `json:"name"`
	Action  string  `json:"action"`
	State   string  `json:"state,omitempty"`
	Seconds float64 `json:"seconds"`
	Error   string  `json:"error,omitempty"`
}

var errCanceled = errors.New("canceled")

// runContainerAction checks the names, asks, and then acts on each
// container in turn, stopping at the first failure. It returns the results
// of the containers it acted on.
func runContainerAction(ctx context.Context, c containerOps, host, action string, names []string, p actionPrompt) ([]containerActionResult, error) {
	verb := containerVerbs[action]

	cs, err := c.Containers(ctx)
	if err != nil {
		if dsm.IsNoAPI(err, dsm.ContainerAPI) {
			return nil, errNoContainerManager
		}
		return nil, err
	}
	if err := checkContainerNames(names, cs); err != nil {
		return nil, err
	}

	if !p.yes {
		if !p.terminal {
			return nil, fmt.Errorf("pass --yes to %s %s without asking", action, strings.Join(names, ", "))
		}
		fmt.Fprintf(p.out, "%s %s on %s? [y/N] ", verb.ask, strings.Join(names, ", "), host)
		answer, _ := bufio.NewReader(p.in).ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
		default:
			return nil, errCanceled
		}
	}

	results := make([]containerActionResult, 0, len(names))
	for _, name := range names {
		fmt.Fprintf(p.out, "%s %s ...", verb.doing, name)
		start := time.Now()
		_, err := c.ContainerAction(ctx, action, name)
		took := time.Since(start).Round(100 * time.Millisecond)
		r := containerActionResult{Name: name, Action: action, Seconds: took.Seconds()}

		if ctx.Err() != nil {
			fmt.Fprintln(p.out, " interrupted; DSM may still finish it.")
			r.Error = "interrupted"
			return append(results, r), ctx.Err()
		}
		state, reason := containerState(ctx, c, name)
		r.State = state
		if err != nil {
			if reason != "" {
				err = fmt.Errorf("%s %s: %s (%w)", action, name, reason, err)
			} else {
				err = fmt.Errorf("%s %s: %w", action, name, err)
			}
			fmt.Fprintf(p.out, " failed.\n")
			r.Error = err.Error()
			return append(results, r), err
		}
		fmt.Fprintf(p.out, " done in %s, %s.\n", took, orDash(state))
		results = append(results, r)
	}
	return results, nil
}

// checkContainerNames fails on names that are not containers, before
// anything is changed, suggesting names that look alike.
func checkContainerNames(names []string, cs []dsm.Container) error {
	known := make([]string, 0, len(cs))
	for _, c := range cs {
		known = append(known, c.Name)
	}
	for _, n := range names {
		if slices.Contains(known, n) {
			continue
		}
		var like []string
		for _, k := range known {
			if strings.Contains(strings.ToLower(k), strings.ToLower(n)) || strings.Contains(strings.ToLower(n), strings.ToLower(k)) {
				like = append(like, k)
			}
		}
		if len(like) > 0 {
			return fmt.Errorf("no container named %q; did you mean %s?", n, strings.Join(like, ", "))
		}
		return fmt.Errorf("no container named %q, see `syno container list`", n)
	}
	return nil
}

// containerState reads the state of a container again after an action, and
// the reason Docker gave when it could not start it.
func containerState(ctx context.Context, c containerOps, name string) (state, reason string) {
	cs, err := c.Containers(ctx)
	if err != nil {
		return "", ""
	}
	for _, x := range cs {
		if x.Name == name {
			return x.State.Status, x.State.Error
		}
	}
	return "", ""
}
