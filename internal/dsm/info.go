package dsm

import (
	"context"
	"net/url"
)

// APIInfo describes one API as listed by SYNO.API.Info.
type APIInfo struct {
	Path       string `json:"path"`
	MinVersion int    `json:"minVersion"`
	MaxVersion int    `json:"maxVersion"`
	// RequestFormat is "JSON" when parameter values must be JSON-encoded
	// (strings quoted, arrays and objects as JSON). Empty means plain values.
	RequestFormat string `json:"requestFormat"`
}

// APIInfo queries SYNO.API.Info, which answers without a session.
// query is a comma separated list of API names, or "all".
func (c *Client) APIInfo(ctx context.Context, query string) (map[string]APIInfo, error) {
	var out map[string]APIInfo
	err := c.CallPath(ctx, "query.cgi", "SYNO.API.Info", 1, "query", url.Values{"query": {query}}, &out)
	return out, err
}
