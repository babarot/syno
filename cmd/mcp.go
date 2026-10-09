package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/babarot/syno/internal/config"
	"github.com/babarot/syno/internal/version"
)

func newMCPCmd() *cobra.Command {
	var allowAPI bool

	c := &cobra.Command{
		Use:   "mcp",
		Short: "Run an MCP server that answers questions about the NAS",
		Long: `Run an MCP server over stdio, so that an AI assistant such as Claude Code can
look at the NAS. The tools only read: the status, the doctor checks, the
containers and the packages, with the same JSON as the --json output of the
commands, and the list of the DSM APIs the NAS provides.

With --allow-api, the syno_api tool also calls DSM APIs, refusing methods
whose name does not say they only read. Even methods that only read can
return secrets, such as the environment variables of containers or the
passwords of notification, DDNS and backup settings, and what a tool returns
is sent to the provider of the AI. Allow it only if you accept that.

Each tool call uses the profile given by --profile or SYNO_PROFILE, or else
the current profile at the time of the call; the host in each answer tells
which NAS answered. The server starts without a profile too, and its tools
then answer that syno login is needed, so the assistant can ask for it.
Register one server per NAS:

  claude mcp add syno -- syno mcp
  claude mcp add syno-office -- syno mcp --profile office`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkMCPStart(); err != nil {
				return err
			}
			return newMCPServer(nasBackend{}, allowAPI).Run(cmd.Context(), &mcp.StdioTransport{})
		},
	}

	c.Flags().BoolVar(&allowAPI, "allow-api", false, "Offer syno_api, which calls DSM APIs and can return secrets to the AI")

	return c
}

// checkMCPStart fails on a broken profiles.yaml, at start where the MCP
// client shows it. A missing profile does not fail: the tools tell the
// assistant to ask for syno login, which a server that failed could not.
func checkMCPStart() error {
	_, err := config.LoadProfiles()
	return err
}

// backend answers the tools. nasBackend asks the NAS; tests use a fake.
type backend interface {
	Status(ctx context.Context) (any, error)
	Doctor(ctx context.Context, skip, only []string) (any, error)
	Containers(ctx context.Context, opts containerListOptions) (any, error)
	Packages(ctx context.Context, outdated bool) (any, error)
	Shares(ctx context.Context, recycle bool) (any, error)
	APIList(ctx context.Context, filter string) (any, error)
	API(ctx context.Context, api, method string, version int, params url.Values) (any, error)
}

type nasBackend struct{}

func (nasBackend) Status(ctx context.Context) (any, error) {
	s, err := loadStatus(ctx)
	if err != nil {
		return nil, err
	}
	return s.report(), nil
}

func (nasBackend) Doctor(ctx context.Context, skip, only []string) (any, error) {
	return runDoctor(ctx, skip, only)
}

func (nasBackend) Containers(ctx context.Context, opts containerListOptions) (any, error) {
	return listContainers(ctx, opts)
}

func (nasBackend) Packages(ctx context.Context, outdated bool) (any, error) {
	return listPackages(ctx, outdated)
}

func (nasBackend) Shares(ctx context.Context, recycle bool) (any, error) {
	return listShares(ctx, recycle)
}

func (nasBackend) APIList(ctx context.Context, filter string) (any, error) {
	return listAPIs(ctx, "", filter)
}

func (nasBackend) API(ctx context.Context, api, method string, version int, params url.Values) (any, error) {
	return callAPI(ctx, api, method, version, params)
}

type (
	noInput     struct{}
	doctorInput struct {
		Skip []string `json:"skip,omitempty" jsonschema:"Checks to turn off, added to doctor.skip in config.yaml"`
		Only []string `json:"only,omitempty" jsonschema:"Run only these checks, ignoring doctor.skip in config.yaml. Cannot be used with skip"`
	}
	containersInput struct {
		Running bool   `json:"running,omitempty" jsonschema:"List only running containers"`
		Project string `json:"project,omitempty" jsonschema:"List only the containers of this Docker Compose project"`
		Usage   bool   `json:"usage,omitempty" jsonschema:"Add cpu_percent (100 is one core) and memory_bytes for running containers. Takes a second or two longer"`
	}
	sharesInput struct {
		Recycle bool `json:"recycle,omitempty" jsonschema:"Also sum what each recycle bin holds, the space emptying it would free. Takes a while for recycle bins with many files"`
	}
	packagesInput struct {
		Outdated bool `json:"outdated,omitempty" jsonschema:"List only the packages with an update"`
	}
	apiListInput struct {
		Filter string `json:"filter,omitempty" jsonschema:"List only the APIs whose name contains this, ignoring case, such as share or backup"`
	}
	apiInput struct {
		API     string            `json:"api" jsonschema:"API name, such as SYNO.Core.Share"`
		Method  string            `json:"method" jsonschema:"Method that only reads: list, get, info, load_info, query, status, or one starting with get_, list_ or load_"`
		Version int               `json:"version,omitempty" jsonschema:"API version (default: the latest the NAS supports)"`
		Params  map[string]string `json:"params,omitempty" jsonschema:"Parameters. For APIs whose request format is JSON, values that are not valid JSON are sent as JSON strings; quote a number the API expects as a string"`
	}
)

// mcpInstructions tells the assistant how the tools fit together, with or
// without syno_api.
func mcpInstructions(allowAPI bool) string {
	other := "Other questions need syno_api, which this server does not offer: say so, and that the user can start the server with syno mcp --allow-api, which lets DSM settings, secrets among them, reach the provider of the AI. syno_api_list shows what the NAS provides."
	if allowAPI {
		other = "For anything else, find an API with syno_api_list and call it with syno_api. DSM does not list methods, so try list, get or info, and another when DSM answers that the method does not exist (code 103). Ask only for what the question needs: the answers can hold secrets."
	}
	return `These tools read a Synology NAS and never change it.
Start with syno_doctor for whether the NAS is healthy, syno_status for space, disks and load, syno_shares for which shared folders use the space, syno_containers and syno_packages for those. ` + other + `
Each call uses the current profile unless the server was started with one, and the user can switch it between calls: the host in each answer tells which NAS answered.
When a tool answers that syno login is needed or that a certificate is not trusted, do not work around it: ask the user to run the command it names in a terminal, since it asks for a password and confirmations.`
}

func newMCPServer(b backend, allowAPI bool) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "syno", Version: version.Version}, &mcp.ServerOptions{Instructions: mcpInstructions(allowAPI)})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "syno_status",
		Description: "Show the Synology NAS: model, DSM version, uptime, temperature, CPU and memory usage, and each storage pool, volume and disk with its status and usage in bytes; disks also have power-on hours, and the life left in percent when DSM estimates it (mostly SSDs). Use this for questions about free space, disks or load.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, any, error) {
		out, err := b.Status(ctx)
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "syno_doctor",
		Description: "Run health checks on the Synology NAS and report each as ok, warn, fail, unknown or skip: storage pools, volume usage, disks and SMART, temperatures, data scrubbing, pending reboot, DSM and package updates, Security Advisor, certificates and containers. " +
			"Use this when asked whether the NAS is healthy or has problems. status is the worst level overall.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in doctorInput) (*mcp.CallToolResult, any, error) {
		out, err := b.Doctor(ctx, in.Skip, in.Only)
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "syno_containers",
		Description: "List the Docker containers of Container Manager on the Synology NAS with their state, health check, Docker Compose project, image and docker ps status. Stopped containers are included unless running is set.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in containersInput) (*mcp.CallToolResult, any, error) {
		out, err := b.Containers(ctx, containerListOptions{RunningOnly: in.Running, Project: in.Project, Usage: in.Usage})
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "syno_packages",
		Description: "List the packages installed on the Synology NAS with the newer version in Package Center, if any, and whether it is a security update. in_store is false for third-party packages, whose updates are unknown.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in packagesInput) (*mcp.CallToolResult, any, error) {
		out, err := b.Packages(ctx, in.Outdated)
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "syno_shares",
		Description: "List the shared folders of the Synology NAS with the space each uses in bytes, the largest first, and whether it is hidden, encrypted, read-only, on USB or has a recycle bin. Use this to find what fills a volume, with recycle to see what emptying the recycle bins would free.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in sharesInput) (*mcp.CallToolResult, any, error) {
		out, err := b.Shares(ctx, in.Recycle)
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "syno_api_list",
		Description: "List the DSM Web APIs the Synology NAS provides, with their path, versions and request format. Use this to find an API for a question the other tools do not answer, such as users, backups or logs, and call it with syno_api when the server offers it.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in apiListInput) (*mcp.CallToolResult, any, error) {
		out, err := b.APIList(ctx, in.Filter)
		return nil, out, err
	})

	if !allowAPI {
		return s
	}

	mcp.AddTool(s, &mcp.Tool{
		Name: "syno_api",
		Description: "Call a DSM Web API on the Synology NAS and return the data field of the response. Only methods whose name says they read are called: list, get, info, load_info, query, status, and ones starting with get_, list_ or load_; others are refused. " +
			"DSM does not list the methods of an API, and answers code 103 for one that does not exist. The data can hold secrets, such as environment variables or passwords in settings: ask only for what the question needs.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in apiInput) (*mcp.CallToolResult, any, error) {
		if !isReadMethod(in.Method) {
			return nil, nil, fmt.Errorf("method %q is refused because its name does not say it only reads; syno_api calls %s, and methods starting with %s",
				in.Method, strings.Join(readMethods, ", "), strings.Join(readPrefixes, ", "))
		}
		params := url.Values{}
		for k, v := range in.Params {
			params.Set(k, v)
		}
		out, err := b.API(ctx, in.API, in.Method, in.Version, params)
		return nil, out, err
	})

	return s
}
