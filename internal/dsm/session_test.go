package dsm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestSessionLosses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		// The API name carries the code to answer.
		fmt.Fprintf(w, `{"success":false,"error":{"code":%s}}`, r.PostForm.Get("api"))
	}))
	defer srv.Close()

	c := NewInsecure(srv.URL)
	ctx := context.Background()
	for _, code := range []string{"105", "106", "107", "119", "102"} {
		_ = c.Call(ctx, code, 1, "get", nil, nil)
	}
	if n := c.SessionLosses(); n != 3 {
		t.Errorf("SessionLosses = %d, want 3 (106, 107, 119)", n)
	}
}

func TestSIDConcurrentUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"data":{}}`)
	}))
	defer srv.Close()

	// go test -race fails here if the sid is not guarded.
	c := NewInsecure(srv.URL)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			if i%2 == 0 {
				c.SetSID(fmt.Sprintf("sid-%d", i))
				return
			}
			_ = c.Call(context.Background(), "SYNO.Test", 1, "get", nil, nil)
		})
	}
	wg.Wait()
}
