package doctor

import (
	"context"
	"fmt"

	"github.com/babarot/syno/internal/dsm"
)

// checkContainers looks at the containers of Container Manager. It is
// optional: on a NAS without Container Manager it is skipped. Stopped
// containers are only counted, since a container stopped on purpose cannot
// be told apart from one that went down.
func checkContainers(ctx context.Context, env *Env) (Result, error) {
	cs, err := env.Source.Containers(ctx)
	if dsm.IsNoAPI(err, dsm.ContainerAPI) {
		return Result{Level: Skip, Summary: "Container Manager is not installed"}, nil
	}
	if err != nil {
		return Result{}, err
	}
	var (
		f       findings
		running int
	)
	for _, c := range cs {
		switch {
		case c.State.Status == "restarting":
			f.add(Fail, "%s keeps restarting", c.Name)
		case c.State.Running && c.Health() == "unhealthy":
			f.add(Fail, "%s is unhealthy", c.Name)
		}
		if c.State.Running {
			running++
		}
	}
	return f.result(fmt.Sprintf("%d running, %d stopped", running, len(cs)-running)), nil
}
