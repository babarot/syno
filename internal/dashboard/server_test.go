package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testAddr  = "127.0.0.1:8765"
	testToken = "secret"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type fakeFetch struct {
	calls atomic.Int32
	err   atomic.Pointer[error]
	// gate, when set, holds every fetch until it is closed.
	gate chan struct{}
}

func (f *fakeFetch) fetch(_ context.Context, profile, panel string) (any, error) {
	n := f.calls.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	if e := f.err.Load(); e != nil {
		return nil, *e
	}
	return map[string]any{"profile": profile, "panel": panel, "n": n}, nil
}

func newTestServer(t *testing.T, f *fakeFetch, c *clock) http.Handler {
	t.Helper()
	s, err := New(context.Background(), Options{
		Addr:     testAddr,
		Profiles: []string{"home"},
		Panels:   []Panel{{Name: "system", Interval: 5 * time.Second, Timeout: time.Second}},
		Fetch:    f.fetch,
		Token:    testToken,
		Now:      c.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s.Handler()
}

func request(h http.Handler, method, path string, edit func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = testAddr
	r.AddCookie(&http.Cookie{Name: "syno_dashboard_8765", Value: testToken})
	if edit != nil {
		edit(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) entry {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var e entry
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestGuard(t *testing.T) {
	h := newTestServer(t, &fakeFetch{}, &clock{now: time.Now()})
	noCookie := func(r *http.Request) { r.Header.Del("Cookie") }

	tests := []struct {
		name string
		do   func() *httptest.ResponseRecorder
		want int
	}{
		{"no cookie", func() *httptest.ResponseRecorder { return request(h, "GET", "/api/config", noCookie) }, 401},
		{"wrong cookie", func() *httptest.ResponseRecorder {
			return request(h, "GET", "/api/config", func(r *http.Request) {
				r.Header.Del("Cookie")
				r.AddCookie(&http.Cookie{Name: "syno_dashboard_8765", Value: "guess"})
			})
		}, 401},
		{"wrong token", func() *httptest.ResponseRecorder { return request(h, "GET", "/?token=guess", noCookie) }, 401},
		{"other host", func() *httptest.ResponseRecorder {
			return request(h, "GET", "/api/config", func(r *http.Request) { r.Host = "evil.example:8765" })
		}, 421},
		{"localhost", func() *httptest.ResponseRecorder {
			return request(h, "GET", "/api/config", func(r *http.Request) { r.Host = "localhost:8765" })
		}, 200},
		{"page", func() *httptest.ResponseRecorder { return request(h, "GET", "/", nil) }, 200},
		{"script", func() *httptest.ResponseRecorder { return request(h, "GET", "/static/app.js", nil) }, 200},
		{"unknown panel", func() *httptest.ResponseRecorder { return request(h, "GET", "/api/panels/home/nope", nil) }, 404},
		{"unknown profile", func() *httptest.ResponseRecorder { return request(h, "GET", "/api/panels/office/system", nil) }, 404},
		{"refresh without header", func() *httptest.ResponseRecorder {
			return request(h, "POST", "/api/panels/home/system/refresh", func(r *http.Request) { r.Header.Set("Origin", "http://"+testAddr) })
		}, 403},
		{"refresh from another origin", func() *httptest.ResponseRecorder {
			return request(h, "POST", "/api/panels/home/system/refresh", func(r *http.Request) {
				r.Header.Set("Origin", "http://evil.example")
				r.Header.Set("X-Syno-Dashboard", "1")
			})
		}, 403},
		{"refresh", func() *httptest.ResponseRecorder {
			return request(h, "POST", "/api/panels/home/system/refresh", func(r *http.Request) {
				r.Header.Set("Origin", "http://"+testAddr)
				r.Header.Set("X-Syno-Dashboard", "1")
			})
		}, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := tt.do()
			if w.Code != tt.want {
				t.Errorf("status %d, want %d: %s", w.Code, tt.want, w.Body)
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Error("CORS header set")
			}
			if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'self'") {
				t.Errorf("CSP = %q", w.Header().Get("Content-Security-Policy"))
			}
		})
	}
}

func TestTokenBecomesCookie(t *testing.T) {
	h := newTestServer(t, &fakeFetch{}, &clock{now: time.Now()})
	w := request(h, "GET", "/?token="+testToken, func(r *http.Request) { r.Header.Del("Cookie") })
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("status %d, location %q", w.Code, w.Header().Get("Location"))
	}
	c := w.Result().Cookies()
	if len(c) != 1 || c[0].Value != testToken || !c[0].HttpOnly || c[0].SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie = %+v", c)
	}
}

func TestPanelCache(t *testing.T) {
	f, c := &fakeFetch{}, &clock{now: time.Now()}
	h := newTestServer(t, f, c)
	get := func() entry { return decode(t, request(h, "GET", "/api/panels/home/system", nil)) }

	if e := get(); e.UpdatedAt == nil || e.IntervalS != 5 {
		t.Fatalf("first answer = %+v", e)
	}
	c.add(2 * time.Second)
	get()
	if n := f.calls.Load(); n != 1 {
		t.Errorf("fetched %d times within the interval, want 1", n)
	}
	c.add(3 * time.Second)
	get()
	if n := f.calls.Load(); n != 2 {
		t.Errorf("fetched %d times after the interval, want 2", n)
	}

	// A failure keeps the last data.
	err := errors.New("NAS is down")
	f.err.Store(&err)
	c.add(5 * time.Second)
	e := get()
	if e.Error != "NAS is down" || e.Data == nil {
		t.Errorf("after a failure: %+v", e)
	}
}

func TestPanelRefresh(t *testing.T) {
	f, c := &fakeFetch{}, &clock{now: time.Now()}
	h := newTestServer(t, f, c)
	refresh := func() {
		decode(t, request(h, "POST", "/api/panels/home/system/refresh", func(r *http.Request) {
			r.Header.Set("Origin", "http://"+testAddr)
			r.Header.Set("X-Syno-Dashboard", "1")
		}))
	}
	refresh()
	c.add(time.Second)
	refresh()
	if n := f.calls.Load(); n != 1 {
		t.Errorf("fetched %d times for refreshes a second apart, want 1", n)
	}
}

func TestPanelSharesFetches(t *testing.T) {
	f := &fakeFetch{gate: make(chan struct{})}
	h := newTestServer(t, f, &clock{now: time.Now()})

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			// Requests that arrive while the fetch runs wait for its data.
			if e := decode(t, request(h, "GET", "/api/panels/home/system", nil)); e.Data == nil {
				t.Error("a request got no data")
			}
		})
	}
	// Let every request reach the server before the fetch ends.
	for f.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(f.gate)
	wg.Wait()
	if n := f.calls.Load(); n != 1 {
		t.Errorf("fetched %d times for requests that arrived together, want 1", n)
	}
}

// The page shows the logo of the README, copied since go:embed cannot
// reach docs/.
func TestLogoMatchesREADME(t *testing.T) {
	want, err := os.ReadFile("../../docs/logo.svg")
	if err != nil {
		t.Fatal(err)
	}
	got, err := web.ReadFile("web/logo.svg")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("internal/dashboard/web/logo.svg differs from docs/logo.svg: copy it again")
	}
}
