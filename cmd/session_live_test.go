package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/babarot/syno/internal/config"
	"github.com/babarot/syno/internal/dsm"
)

// fakeDSM accepts the sessions it handed out until expire, and answers
// SYNO.Test get with 119 for any other session.
type fakeDSM struct {
	mu     sync.Mutex
	valid  map[string]bool
	logins atomic.Int32
	refuse atomic.Bool // answer logins with a wrong password
}

func newFakeDSM(t *testing.T) (*fakeDSM, *httptest.Server) {
	d := &fakeDSM{valid: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		switch r.PostForm.Get("api") {
		case "SYNO.API.Auth":
			if d.refuse.Load() {
				fmt.Fprint(w, `{"success":false,"error":{"code":400}}`)
				return
			}
			sid := fmt.Sprintf("sid-%d", d.logins.Add(1))
			d.mu.Lock()
			d.valid[sid] = true
			d.mu.Unlock()
			fmt.Fprintf(w, `{"success":true,"data":{"sid":%q}}`, sid)
		case "SYNO.Core.Hardware.NeedReboot", "SYNO.Test":
			d.mu.Lock()
			ok := d.valid[r.PostForm.Get("_sid")]
			d.mu.Unlock()
			if !ok {
				fmt.Fprint(w, `{"success":false,"error":{"code":119}}`)
				return
			}
			fmt.Fprint(w, `{"success":true,"data":{}}`)
		default:
			fmt.Fprint(w, `{"success":false,"error":{"code":102}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return d, srv
}

func (d *fakeDSM) expire() {
	d.mu.Lock()
	defer d.mu.Unlock()
	clear(d.valid)
}

func testCall(ctx context.Context, c *dsm.Client) error {
	return c.Call(ctx, "SYNO.Test", 1, "get", nil, nil)
}

// parallel runs fn n times at once and returns the errors.
func parallel(n int, fn func() error) []error {
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { errs[i] = fn() })
	}
	wg.Wait()
	return errs
}

func TestLiveSessionLogsInOnce(t *testing.T) {
	d, srv := newFakeDSM(t)
	s := newLiveSession(&config.Profile{URL: srv.URL, User: "admin"}, &fakeStore{})
	ctx := context.Background()

	for _, err := range parallel(10, func() error { return s.do(ctx, func(c *dsm.Client) error { return testCall(ctx, c) }) }) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := d.logins.Load(); n != 1 {
		t.Errorf("logged in %d times, want 1", n)
	}
}

func TestLiveSessionLogsInAgainOnce(t *testing.T) {
	d, srv := newFakeDSM(t)
	s := newLiveSession(&config.Profile{URL: srv.URL, User: "admin"}, &fakeStore{})
	ctx := context.Background()
	if err := s.do(ctx, func(c *dsm.Client) error { return testCall(ctx, c) }); err != nil {
		t.Fatal(err)
	}

	d.expire()
	for _, err := range parallel(10, func() error { return s.do(ctx, func(c *dsm.Client) error { return testCall(ctx, c) }) }) {
		if err != nil {
			t.Fatal(err)
		}
	}
	// The saved session is the expired one, so the second login uses the
	// password; the callers that noticed together share it.
	if n := d.logins.Load(); n != 2 {
		t.Errorf("logged in %d times, want 2", n)
	}
}

func TestLiveSessionNoticesSwallowedErrors(t *testing.T) {
	d, srv := newFakeDSM(t)
	s := newLiveSession(&config.Profile{URL: srv.URL, User: "admin"}, &fakeStore{})
	ctx := context.Background()
	if err := s.do(ctx, func(c *dsm.Client) error { return testCall(ctx, c) }); err != nil {
		t.Fatal(err)
	}

	// Like doctor, which turns the errors of its checks into results.
	d.expire()
	var runs, failed int
	err := s.do(ctx, func(c *dsm.Client) error {
		runs++
		if testCall(ctx, c) != nil {
			failed++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if runs != 2 || failed != 1 {
		t.Errorf("ran %d times with %d failures, want 2 runs and the second one passing", runs, failed)
	}
}

func TestLiveSessionKeepsOtherErrors(t *testing.T) {
	d, srv := newFakeDSM(t)
	s := newLiveSession(&config.Profile{URL: srv.URL, User: "admin"}, &fakeStore{})
	ctx := context.Background()

	runs := 0
	err := s.do(ctx, func(c *dsm.Client) error {
		runs++
		return c.Call(ctx, "SYNO.Missing", 1, "get", nil, nil)
	})
	if !dsm.IsNoAPI(err, "SYNO.Missing") {
		t.Errorf("err = %v, want 102", err)
	}
	if runs != 1 || d.logins.Load() != 1 {
		t.Errorf("ran %d times and logged in %d times, want 1 and 1", runs, d.logins.Load())
	}
}

func TestLiveSessionRetriesOpening(t *testing.T) {
	d, srv := newFakeDSM(t)
	s := newLiveSession(&config.Profile{URL: srv.URL, User: "admin"}, &fakeStore{})
	ctx := context.Background()

	d.refuse.Store(true)
	if err := s.do(ctx, func(*dsm.Client) error { return nil }); err == nil {
		t.Fatal("opened with a refused login")
	}
	d.refuse.Store(false)
	if err := s.do(ctx, func(c *dsm.Client) error { return testCall(ctx, c) }); err != nil {
		t.Errorf("a failed first login is not tried again: %v", err)
	}
}
