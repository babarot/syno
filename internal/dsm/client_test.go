package dsm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
