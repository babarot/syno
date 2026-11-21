package discover

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
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
// SYNO.API.Info answers without authentication. When neither answers, it
// returns the error from the HTTPS attempt.
func probeDSM(ctx context.Context, ip netip.Addr) (string, error) {
	var first error
	for _, base := range []string{
		fmt.Sprintf("https://%s:5001", ip),
		fmt.Sprintf("http://%s:5000", ip),
	} {
		err := apiInfo(ctx, base)
		if err == nil {
			return base, nil
		}
		if first == nil {
			first = err
		}
	}
	return "", first
}

func apiInfo(ctx context.Context, base string) error {
	url := base + "/webapi/query.cgi?api=SYNO.API.Info&version=1&method=query&query=SYNO.API.Auth"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var body struct {
		Success bool `json:"success"`
		Data    map[string]json.RawMessage
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	if _, ok := body.Data["SYNO.API.Auth"]; !body.Success || !ok {
		return errors.New("no DSM Web API")
	}
	return nil
}
