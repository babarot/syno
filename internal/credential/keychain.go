// Package credential keeps DSM secrets in the macOS keychain via security(1).
package credential

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const service = "syno"

// ErrNotFound means no item exists for the account.
var ErrNotFound = errors.New("not found in keychain")

func passwordAccount(host, user string) string { return user + "@" + host }
func deviceAccount(host, user string) string   { return user + "@" + host + "#device" }

func Password(host, user string) (string, error) { return get(passwordAccount(host, user)) }
func DeviceID(host, user string) (string, error) { return get(deviceAccount(host, user)) }

func SetPassword(host, user, secret string) error { return set(passwordAccount(host, user), secret) }
func SetDeviceID(host, user, secret string) error { return set(deviceAccount(host, user), secret) }

func get(account string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.Command("security", "find-generic-password", "-s", service, "-a", account, "-w")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(stderr.String(), "could not be found") {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("security find-generic-password: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func set(account, secret string) error {
	// -U updates the item when it already exists.
	cmd := exec.Command("security", "add-generic-password", "-U", "-s", service, "-a", account, "-w", secret)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("security add-generic-password: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
