// Package credential keeps DSM secrets in the OS keyring: the Keychain on
// macOS, the Secret Service on Linux and the Credential Manager on Windows.
package credential

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const service = "syno"

// ErrNotFound means no item exists for the account.
var ErrNotFound = errors.New("not found in keyring")

func passwordAccount(host, user string) string { return user + "@" + host }
func deviceAccount(host, user string) string   { return user + "@" + host + "#device" }

func Password(host, user string) (string, error) { return get(passwordAccount(host, user)) }
func DeviceID(host, user string) (string, error) { return get(deviceAccount(host, user)) }

func SetPassword(host, user, secret string) error { return set(passwordAccount(host, user), secret) }
func SetDeviceID(host, user, secret string) error { return set(deviceAccount(host, user), secret) }

// Delete removes the password and the device token of the account.
// Items that do not exist are ignored.
func Delete(host, user string) error {
	for _, account := range []string{passwordAccount(host, user), deviceAccount(host, user)} {
		if err := keyring.Delete(service, account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
			return fmt.Errorf("delete %s from keyring: %w", account, err)
		}
	}
	return nil
}

func get(account string) (string, error) {
	secret, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read %s from keyring: %w (set SYNO_PASSWORD if no keyring is available)", account, err)
	}
	return secret, nil
}

// set stores secret. On macOS go-keyring hands it to security(1) through
// stdin, so it never shows up in the process list.
func set(account, secret string) error {
	if err := keyring.Set(service, account, secret); err != nil {
		return fmt.Errorf("save %s to keyring: %w", account, err)
	}
	return nil
}
