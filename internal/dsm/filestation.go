package dsm

import (
	"context"
	"encoding/json"
	"net/url"
	"time"
)

// DirSize is SYNO.FileStation.DirSize status. Before Finished, the counts
// are what has been summed so far.
type DirSize struct {
	Finished  bool `json:"finished"`
	NumDir    Num  `json:"num_dir"`
	NumFile   Num  `json:"num_file"`
	TotalSize Num  `json:"total_size"` // bytes
}

// The task API of SYNO.FileStation.DirSize. Version 1 answers with HTTP 502.
const dirSizeVersion = 2

// StartDirSize starts summing the size of paths in the background on the
// NAS and returns the task ID. Paths are like "/share/folder". A path that
// does not exist is not an error: it sums to 0.
func (c *Client) StartDirSize(ctx context.Context, paths ...string) (string, error) {
	b, err := json.Marshal(paths)
	if err != nil {
		return "", err
	}
	var out struct {
		TaskID string `json:"taskid"`
	}
	params := url.Values{"path": {string(b)}}
	if err := c.Call(ctx, "SYNO.FileStation.DirSize", dirSizeVersion, "start", params, &out); err != nil {
		return "", err
	}
	return out.TaskID, nil
}

func (c *Client) DirSizeStatus(ctx context.Context, taskID string) (*DirSize, error) {
	var out DirSize
	params := url.Values{"taskid": {quote(taskID)}}
	if err := c.Call(ctx, "SYNO.FileStation.DirSize", dirSizeVersion, "status", params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// StopDirSize ends the task, finished or not, so that it does not stay on
// the NAS.
func (c *Client) StopDirSize(ctx context.Context, taskID string) error {
	params := url.Values{"taskid": {quote(taskID)}}
	return c.Call(ctx, "SYNO.FileStation.DirSize", dirSizeVersion, "stop", params, nil)
}

// MeasureDir sums the size of path, checking every interval until the NAS
// is done. The task is stopped in every case, even when ctx is canceled,
// in which case the last counts are returned with the error.
func (c *Client) MeasureDir(ctx context.Context, path string, interval time.Duration) (*DirSize, error) {
	id, err := c.StartDirSize(ctx, path)
	if err != nil {
		return nil, err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = c.StopDirSize(ctx, id)
	}()

	last := &DirSize{}
	for {
		s, err := c.DirSizeStatus(ctx, id)
		if err != nil {
			return last, err
		}
		if s.Finished {
			return s, nil
		}
		last = s
		select {
		case <-ctx.Done():
			return last, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// quote makes a JSON string of s, for APIs that take JSON parameters.
func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
