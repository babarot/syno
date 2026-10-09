package credential

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestCredential(t *testing.T) {
	keyring.MockInit()
	const host, user = "https://192.168.1.10:5001", "admin"

	if _, err := Password(host, user); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Password before set: err = %v, want ErrNotFound", err)
	}

	if err := SetPassword(host, user, "secret"); err != nil {
		t.Fatal(err)
	}
	if err := SetDeviceID(host, user, "device-token"); err != nil {
		t.Fatal(err)
	}
	if got, err := Password(host, user); err != nil || got != "secret" {
		t.Errorf("Password = %q, %v", got, err)
	}
	if got, err := DeviceID(host, user); err != nil || got != "device-token" {
		t.Errorf("DeviceID = %q, %v", got, err)
	}

	if err := Delete(host, user); err != nil {
		t.Fatal(err)
	}
	if _, err := Password(host, user); !errors.Is(err, ErrNotFound) {
		t.Errorf("Password after delete: err = %v, want ErrNotFound", err)
	}
	if _, err := DeviceID(host, user); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeviceID after delete: err = %v, want ErrNotFound", err)
	}

	// Deleting again is not an error.
	if err := Delete(host, user); err != nil {
		t.Errorf("second Delete: %v", err)
	}
}
