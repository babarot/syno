package dsm

import (
	"errors"
	"fmt"
)

// APIError is a DSM Web API failure ({"success":false,"error":{"code":N}}).
type APIError struct {
	API    string
	Method string
	Code   int
}

// Codes shared by every API.
var commonErrors = map[int]string{
	100: "unknown error",
	101: "invalid parameter",
	102: "API does not exist",
	103: "method does not exist",
	104: "version not supported",
	105: "permission denied (an administrator account is required)",
	106: "session timeout",
	107: "session interrupted by duplicate login",
	119: "not logged in",
}

// Codes returned by SYNO.API.Auth.
var authErrors = map[int]string{
	400: "wrong account or password",
	401: "account disabled",
	402: "permission denied",
	403: "2FA code required (run `syno login --otp`)",
	404: "2FA code rejected",
	406: "2FA must be enabled for this account",
	407: "IP address blocked",
	408: "password expired, change it in DSM",
	409: "password expired, change it in DSM",
	410: "password must be changed in DSM",
}

func (e *APIError) Error() string {
	msg, ok := commonErrors[e.Code]
	if e.API == "SYNO.API.Auth" {
		if m, ok2 := authErrors[e.Code]; ok2 {
			msg, ok = m, true
		}
	}
	if !ok {
		msg = "error"
	}
	return fmt.Sprintf("%s.%s: %s (code %d)", e.API, e.Method, msg, e.Code)
}

// IsOTPRequired reports whether the login failed because a 2FA code is needed.
func IsOTPRequired(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.API == "SYNO.API.Auth" && e.Code == 403
}
