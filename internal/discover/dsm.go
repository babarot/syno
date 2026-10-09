package discover

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"time"
)

var httpClient = &http.Client{
	Timeout: 2 * time.Second,
	Transport: &http.Transport{
		// Not verified: DSM often has a self-signed certificate, and the
		// probe only reads SYNO.API.Info, sending no credentials.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	},
}

// probeDSM checks whether ip serves the DSM Web API and returns its base URL.
// HTTPS is tried first so that later logins do not send passwords in clear text.
// SYNO.API.Info answers without authentication.
func probeDSM(ctx context.Context, ip netip.Addr) string {
	for _, base := range []string{
		fmt.Sprintf("https://%s:5001", ip),
		fmt.Sprintf("http://%s:5000", ip),
	} {
		if apiInfoOK(ctx, base) {
			return base
		}
	}
	return ""
}

func apiInfoOK(ctx context.Context, base string) bool {
	url := base + "/webapi/query.cgi?api=SYNO.API.Info&version=1&method=query&query=SYNO.API.Auth"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body struct {
		Success bool `json:"success"`
		Data    map[string]json.RawMessage
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	_, ok := body.Data["SYNO.API.Auth"]
	return body.Success && ok
}
