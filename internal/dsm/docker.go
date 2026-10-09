package dsm

import (
	"context"
	"net/url"
)

// Container is one entry of SYNO.Docker.Container list. Only the fields syno
// shows are decoded. The response may also carry the environment variables
// of the container, which can hold secrets, so they are left out on purpose.
type Container struct {
	Name string `json:"name"`
	// Image is like "nginx:latest".
	Image string `json:"image"`
	// Status is DSM's coarse state: "running" or "stopped".
	Status string `json:"status"`
	// UpStatus is the STATUS column of `docker ps`, like "Up 10 days".
	UpStatus string `json:"up_status"`
	// State is the State of `docker inspect`.
	State struct {
		// Status is Docker's state: "running", "exited", "restarting",
		// "paused", "created" or "dead".
		Status   string `json:"Status"`
		Running  bool   `json:"Running"`
		ExitCode int    `json:"ExitCode"`
		Health   *struct {
			// Status is "starting", "healthy" or "unhealthy".
			Status        string `json:"Status"`
			FailingStreak int    `json:"FailingStreak"`
		} `json:"Health"`
	} `json:"State"`
	Labels map[string]string `json:"Labels"`
}

// Health returns the health check status, or "" when the container has no
// health check.
func (c Container) Health() string {
	if c.State.Health == nil {
		return ""
	}
	return c.State.Health.Status
}

// Project returns the Docker Compose project the container belongs to, or "".
func (c Container) Project() string {
	return c.Labels["com.docker.compose.project"]
}

// ContainerAPI is the API of Container Manager that lists containers.
const ContainerAPI = "SYNO.Docker.Container"

// Containers lists every container of Container Manager, stopped ones
// included.
func (c *Client) Containers(ctx context.Context) ([]Container, error) {
	// The API is JSON-format, so the string value needs quotes. "type" accepts
	// "all" and "running" but returns stopped containers either way, so
	// callers filter by State.Running themselves.
	params := url.Values{
		"limit":  {"-1"},
		"offset": {"0"},
		"type":   {`"all"`},
	}
	var out struct {
		Containers []Container `json:"containers"`
	}
	if err := c.Call(ctx, ContainerAPI, 1, "list", params, &out); err != nil {
		return nil, err
	}
	return out.Containers, nil
}
