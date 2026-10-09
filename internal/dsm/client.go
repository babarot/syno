// Package dsm is a small client for the Synology DSM Web API.
package dsm

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const session = "syno"

// Client talks to one DSM host. Base is like "https://192.168.1.10:5001".
type Client struct {
	Base string
	http *http.Client
	sid  string
}

func New(base string) *Client {
	return &Client{
		Base: strings.TrimSuffix(base, "/"),
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				// DSM ships with a self-signed certificate by default.
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

// LoginOptions holds the credentials for SYNO.API.Auth.
type LoginOptions struct {
	User     string
	Password string
	// OTP is the 2FA code. Needed only when 2FA is on and no DeviceID is known.
	OTP string
	// DeviceID is the token DSM issues after a successful 2FA login, which
	// lets later logins skip the OTP.
	DeviceID string
}

// Login opens a session and returns the device token DSM issued, if any.
func (c *Client) Login(ctx context.Context, o LoginOptions) (deviceID string, err error) {
	params := url.Values{
		"account": {o.User},
		"passwd":  {o.Password},
		"session": {session},
		"format":  {"sid"},
	}
	if o.OTP != "" {
		params.Set("otp_code", o.OTP)
		params.Set("enable_device_token", "yes")
		params.Set("device_name", "syno-cli")
	}
	if o.DeviceID != "" {
		params.Set("device_name", "syno-cli")
		params.Set("device_id", o.DeviceID)
	}
	var out struct {
		SID string `json:"sid"`
		DID string `json:"did"`
	}
	if err := c.Call(ctx, "SYNO.API.Auth", 6, "login", params, &out); err != nil {
		return "", err
	}
	c.sid = out.SID
	return out.DID, nil
}

// Logout closes the session. Errors are ignored by most callers.
func (c *Client) Logout(ctx context.Context) error {
	if c.sid == "" {
		return nil
	}
	err := c.Call(ctx, "SYNO.API.Auth", 6, "logout", url.Values{"session": {session}}, nil)
	c.sid = ""
	return err
}

// Raw calls an API at path (relative to /webapi/, e.g. "entry.cgi") and
// returns the "data" field undecoded.
func (c *Client) Raw(ctx context.Context, path, api string, version int, method string, params url.Values) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.CallPath(ctx, path, api, version, method, params, &raw)
	return raw, err
}

// Call invokes api.method on entry.cgi, where almost every DSM 7 API lives,
// and decodes the "data" field into out.
func (c *Client) Call(ctx context.Context, api string, version int, method string, params url.Values, out any) error {
	return c.CallPath(ctx, "entry.cgi", api, version, method, params, out)
}

// CallPath is Call for an API served at another path, as reported by
// SYNO.API.Info. Parameters are sent as a POST form so that passwords stay
// out of URLs.
func (c *Client) CallPath(ctx context.Context, path, api string, version int, method string, params url.Values, out any) error {
	form := url.Values{}
	for k, v := range params {
		form[k] = v
	}
	form.Set("api", api)
	form.Set("version", strconv.Itoa(version))
	form.Set("method", method)
	if c.sid != "" {
		form.Set("_sid", c.sid)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/webapi/"+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s.%s: HTTP %s", api, method, resp.Status)
	}

	var body struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("%s.%s: decode response: %w", api, method, err)
	}
	if !body.Success {
		code := 0
		if body.Error != nil {
			code = body.Error.Code
		}
		return &APIError{API: api, Method: method, Code: code}
	}
	if out == nil || len(body.Data) == 0 {
		return nil
	}
	if raw, ok := out.(*json.RawMessage); ok {
		*raw = body.Data
		return nil
	}
	if err := json.Unmarshal(body.Data, out); err != nil {
		return fmt.Errorf("%s.%s: decode data: %w", api, method, err)
	}
	return nil
}
