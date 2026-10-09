package cmd

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/babarot/syno/internal/version"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run an MCP server that answers questions about the NAS",
		Long: `Run an MCP server over stdio, so that an AI assistant such as Claude Code can
look at the NAS. The tools only read: the status, the doctor checks, the
containers and the packages, with the same JSON as the --json output of the
commands. Nothing on the NAS is changed, and syno api is not offered.

The NAS is the one of the profile chosen at start (--profile, then
SYNO_PROFILE, then the current profile). Register one server per NAS:

  claude mcp add syno -- syno mcp
  claude mcp add syno-office -- syno mcp --profile office`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fail at start, where the MCP client shows the error, rather than
			// on every tool call.
			if _, _, err := selectProfile(); err != nil {
				return err
			}
			return newMCPServer(nasBackend{}).Run(cmd.Context(), &mcp.StdioTransport{})
		},
	}
}

// backend answers the tools. nasBackend asks the NAS; tests use a fake.
type backend interface {
	Status(ctx context.Context) (any, error)
	Doctor(ctx context.Context, skip, only []string) (any, error)
	Containers(ctx context.Context, runningOnly bool, project string) (any, error)
	Packages(ctx context.Context, outdated bool) (any, error)
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

func (nasBackend) Containers(ctx context.Context, runningOnly bool, project string) (any, error) {
	return listContainers(ctx, runningOnly, project)
}

func (nasBackend) Packages(ctx context.Context, outdated bool) (any, error) {
	return listPackages(ctx, outdated)
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
	}
	packagesInput struct {
		Outdated bool `json:"outdated,omitempty" jsonschema:"List only the packages with an update"`
	}
)

func newMCPServer(b backend) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "syno", Version: version.Version}, nil)
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "syno_status",
		Description: "Show the Synology NAS: model, DSM version, uptime, temperature, CPU and memory usage, and each storage pool, volume and disk with its status and usage in bytes. Use this for questions about free space, disks or load.",
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
		out, err := b.Containers(ctx, in.Running, in.Project)
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

	return s
}
