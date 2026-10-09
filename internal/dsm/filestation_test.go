package dsm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeDirSize answers SYNO.FileStation.DirSize: status reports unfinished
// until it has been asked polls times.
type fakeDirSize struct {
	t        *testing.T
	polls    int
	statusFn func(n int) string

	mu      sync.Mutex
	paths   []string
	asked   int
	stopped []string
}

func (f *fakeDirSize) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		f.t.Fatal(err)
	}
	if v := r.PostForm.Get("version"); v != "2" {
		f.t.Errorf("version = %s, want 2", v)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch m := r.PostForm.Get("method"); m {
	case "start":
		f.paths = append(f.paths, r.PostForm.Get("path"))
		_, _ = w.Write([]byte(`{"success":true,"data":{"taskid":"T1"}}`))
	case "status":
		if id := r.PostForm.Get("taskid"); id != `"T1"` {
			f.t.Errorf("taskid = %s, want a JSON string", id)
		}
		f.asked++
		if f.statusFn != nil {
			_, _ = w.Write([]byte(f.statusFn(f.asked)))
			return
		}
		finished := f.asked >= f.polls
		body := `{"success":true,"data":{"finished":false,"num_dir":1,"num_file":10,"total_size":100}}`
		if finished {
			body = `{"success":true,"data":{"finished":true,"num_dir":2,"num_file":20,"total_size":2048}}`
		}
		_, _ = w.Write([]byte(body))
	case "stop":
		f.stopped = append(f.stopped, r.PostForm.Get("taskid"))
		_, _ = w.Write([]byte(`{"success":true}`))
	default:
		f.t.Errorf("unexpected method %s", m)
	}
}

func serveDirSize(t *testing.T, f *fakeDirSize) *Client {
	t.Helper()
	f.t = t
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return NewInsecure(srv.URL)
}

func TestMeasureDir(t *testing.T) {
	f := &fakeDirSize{polls: 3}
	s, err := serveDirSize(t, f).MeasureDir(context.Background(), "/media/#recycle", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Finished || s.NumFile != 20 || s.TotalSize != 2048 {
		t.Errorf("got %+v", s)
	}
	if len(f.paths) != 1 || f.paths[0] != `["/media/#recycle"]` {
		t.Errorf("path = %v, want a JSON array", f.paths)
	}
	if len(f.stopped) != 1 {
		t.Errorf("stopped %d times, want once", len(f.stopped))
	}
}

func TestMeasureDirStopsWhenCanceled(t *testing.T) {
	f := &fakeDirSize{polls: 1 << 30}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	s, err := serveDirSize(t, f).MeasureDir(ctx, "/media/#recycle", 5*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	if s == nil || s.NumFile != 10 {
		t.Errorf("got %+v, want the last counts", s)
	}
	if len(f.stopped) != 1 {
		t.Errorf("stopped %d times, want once even after the context ended", len(f.stopped))
	}
}

func TestMeasureDirStopsOnError(t *testing.T) {
	f := &fakeDirSize{statusFn: func(int) string { return `{"success":false,"error":{"code":599}}` }}
	if _, err := serveDirSize(t, f).MeasureDir(context.Background(), "/media/#recycle", time.Millisecond); err == nil {
		t.Fatal("want the status error")
	}
	if len(f.stopped) != 1 {
		t.Errorf("stopped %d times, want once", len(f.stopped))
	}
}
