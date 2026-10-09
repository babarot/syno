// Package dsm is a small client for the Synology DSM Web API.
package dsm

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const session = "syno"

// Client talks to one DSM host. Base is like "https://192.168.1.10:5001".
type Client struct {
	Base string
	http *http.Client

	// mu guards sid, which a long-running command replaces while other
	// calls are in flight.
	mu  sync.RWMutex
	sid string
	// lost counts the answers that said the session is gone.
	lost atomic.Uint64
}

// New returns a client that verifies the server certificate. With an empty
// pin the certificate must be trusted by the system and match the host
// name. With a pin ("sha256/<base64>", see Pin) the server's public key must
// match it instead, which is how self-signed certificates and access by IP
// address are trusted.
func New(base, pin string) *Client {
	cfg := &tls.Config{}
	if pin != "" {
		cfg = pinnedConfig(base, pin)
	}
	return newClient(base, cfg)
}

// NewInsecure returns a client that does not verify the server certificate.
// Use it only for calls that send no credentials and whose answers do no
// harm if forged, such as SYNO.API.Info.
func NewInsecure(base string) *Client {
	return newClient(base, &tls.Config{InsecureSkipVerify: true})
}

func newClient(base string, cfg *tls.Config) *Client {
	return &Client{
		Base: strings.TrimSuffix(base, "/"),
		http: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{TLSClientConfig: cfg},
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
	c.SetSID(out.SID)
	return out.DID, nil
}

// Logout closes the session. Errors are ignored by most callers.
func (c *Client) Logout(ctx context.Context) error {
	if c.SID() == "" {
		return nil
	}
	err := c.Call(ctx, "SYNO.API.Auth", 6, "logout", url.Values{"session": {session}}, nil)
	c.SetSID("")
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
	maps.Copy(form, params)
	form.Set("api", api)
	form.Set("version", strconv.Itoa(version))
	form.Set("method", method)
	if sid := c.SID(); sid != "" {
		form.Set("_sid", sid)
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
		err := &APIError{API: api, Method: method, Code: code}
		if IsSessionGone(err) {
			c.lost.Add(1)
		}
		return err
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

// SID returns the session ID of the logged in session, or "".
func (c *Client) SID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sid
}

// SetSID resumes a session saved from an earlier login.
func (c *Client) SetSID(sid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sid = sid
}

// SessionLosses counts the answers so far that said the session is gone.
// Comparing it before and after some work tells whether the session was
// lost during it, even where the error itself was turned into a message,
// as in the doctor results.
func (c *Client) SessionLosses() uint64 { return c.lost.Load() }
