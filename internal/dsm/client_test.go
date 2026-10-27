package dsm

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestCallTimeoutHint(t *testing.T) {
	// DSM answers only after its disks spin up.
	spunUp := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-spunUp
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := NewInsecure(srv.URL).Call(ctx, "SYNO.Core.System", 1, "info", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "hibernation") {
		t.Errorf("timeout: got %v", err)
	}

	// Other failures, like nothing listening, get no hint.
	close(spunUp)
	srv.Close()
	err = NewInsecure(srv.URL).Call(context.Background(), "SYNO.Core.System", 1, "info", nil, nil)
	if err == nil || strings.Contains(err.Error(), "hibernation") {
		t.Errorf("refused: got %v", err)
	}
}

// timeout is an error that reports a timeout, like the one a dialer
// returns.
type timeout struct{}

func (timeout) Error() string { return "i/o timeout" }
func (timeout) Timeout() bool { return true }

func TestDialTimeoutHint(t *testing.T) {
	// A NAS that is off: the connection is never made.
	err := explainTimeout(&url.Error{Op: "Post", URL: "https://192.168.1.10:5001/webapi/entry.cgi",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: timeout{}}})
	if !strings.Contains(err.Error(), "could not connect") || strings.Contains(err.Error(), "hibernation") {
		t.Errorf("got %v", err)
	}
}
