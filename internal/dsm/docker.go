package dsm

import (
	"context"
	"encoding/json"
	"fmt"
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
		// Error is why Docker could not start the container, such as a
		// network that no longer exists.
		Error  string `json:"Error"`
		Health *struct {
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

// ContainerActionResult is what start, stop and restart answer: the usage
// of the container after the action.
type ContainerActionResult struct {
	Name          string `json:"name"`
	CPU           Num    `json:"cpu"`
	Memory        Num    `json:"memory"` // bytes
	MemoryPercent Num    `json:"memoryPercent"`
}

// ContainerAction starts, stops or restarts a container. DSM answers when
// the action is done, which takes several seconds, and the actions are
// idempotent: starting a running container does nothing. When a container
// fails to start, DSM answers with code 1301 and no reason; the reason is in
// the container's State.Error.
func (c *Client) ContainerAction(ctx context.Context, action, name string) (*ContainerActionResult, error) {
	switch action {
	case "start", "stop", "restart":
	default:
		return nil, fmt.Errorf("unknown container action %q", action)
	}
	b, err := json.Marshal(name)
	if err != nil {
		return nil, err
	}
	var out ContainerActionResult
	if err := c.Call(ctx, ContainerAPI, 1, action, url.Values{"name": {string(b)}}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
