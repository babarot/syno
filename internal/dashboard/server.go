// Package dashboard serves the web page of syno dashboard on localhost.
//
// The page polls one JSON endpoint per panel and profile. The server asks
// the NAS only when a page asks for a panel whose last answer is older than
// the panel's interval, so nothing reaches the NAS while no page is open.
// The server knows nothing of DSM: the caller fetches the panels.
package dashboard

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

//go:embed web
var web embed.FS

// Panel is one part of the page with the pace it is refreshed at.
type Panel struct {
	Name     string
	Interval time.Duration
	// Timeout bounds one fetch of the panel.
	Timeout time.Duration
}

// FetchFunc returns the data of a panel of a profile, as JSON-encodable.
type FetchFunc func(ctx context.Context, profile, panel string) (any, error)

// Options configures a Server.
type Options struct {
	// Addr is the address the listener got, like "127.0.0.1:52817". Only
	// requests for this host and port are answered.
	Addr     string
	Profiles []string
	Panels   []Panel
	Fetch    FetchFunc
	// Token is the secret the URL carries. A random one is made when empty.
	Token string
	// Now is for tests.
	Now func() time.Time
}

// refreshGap is how soon after a fetch a refresh button fetches again.
const refreshGap = 10 * time.Second

// Server is the dashboard's HTTP server.
type Server struct {
	ctx      context.Context
	opts     Options
	hosts    []string
	cookie   string
	panels   map[string]Panel
	profiles map[string]bool
	now      func() time.Time

	fetches singleflight.Group
	mu      sync.Mutex
	entries map[string]*entry
}

// entry is what the server knows of one panel of one profile.
type entry struct {
	// Data is the last data fetched without an error.
	Data any `json:"data"`
	// Error is why the last fetch failed, if it did; Data stays as it was.
	Error string `json:"error,omitempty"`
	// UpdatedAt is when Data was fetched.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	// DurationMS is how long the last fetch took.
	DurationMS int64 `json:"duration_ms"`
	IntervalS  int   `json:"interval_s"`

	// tried is when the last fetch started, which decides the next one
	// whether it failed or not.
	tried time.Time
	// fetching is set while a fetch runs.
	fetching bool
}

// New returns a server whose fetches end with ctx.
func New(ctx context.Context, o Options) (*Server, error) {
	_, port, err := net.SplitHostPort(o.Addr)
	if err != nil {
		return nil, err
	}
	if o.Token == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		o.Token = hex.EncodeToString(b)
	}
	s := &Server{
		ctx:  ctx,
		opts: o,
		// The browser sends the host it was given; anything else is a page
		// on another site whose name was made to resolve to 127.0.0.1.
		hosts:    []string{"127.0.0.1:" + port, "localhost:" + port},
		cookie:   "syno_dashboard_" + port,
		panels:   map[string]Panel{},
		profiles: map[string]bool{},
		now:      o.Now,
		entries:  map[string]*entry{},
	}
	if s.now == nil {
		s.now = time.Now
	}
	for _, p := range o.Panels {
		s.panels[p.Name] = p
	}
	for _, p := range o.Profiles {
		s.profiles[p] = true
	}
	return s, nil
}

// URL is the address to open, with the token.
func (s *Server) URL() string {
	return "http://" + s.opts.Addr + "/?token=" + s.opts.Token
}

// Serve answers on ln until ctx ends.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			return err
		}
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

// Handler returns the routes behind the checks of guard.
func (s *Server) Handler() http.Handler {
	static, err := fs.Sub(web, "web")
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, "index.html")
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /api/config", s.config)
	mux.HandleFunc("GET /api/panels/{profile}/{panel}", s.panel(false))
	mux.HandleFunc("POST /api/panels/{profile}/{panel}/refresh", s.panel(true))
	return s.guard(mux)
}

// guard lets through only requests from the page opened with the token.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")

		if !slices.Contains(s.hosts, r.Host) {
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}

		if t := r.URL.Query().Get("token"); t != "" && r.URL.Path == "/" {
			if !s.validToken(t) {
				http.Error(w, "wrong token: open the URL that syno dashboard printed", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: s.cookie, Value: s.opts.Token, Path: "/",
				HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
			// Drop the token from the address bar and the history.
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		c, err := r.Cookie(s.cookie)
		if err != nil || !s.validToken(c.Value) {
			http.Error(w, "open the URL that syno dashboard printed, with its token", http.StatusUnauthorized)
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// SameSite already keeps the cookie from other sites; these make
			// sure the request came from the page's own script.
			if r.Header.Get("Origin") != "http://"+r.Host || r.Header.Get("X-Syno-Dashboard") != "1" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validToken(t string) bool {
	return subtle.ConstantTimeCompare([]byte(t), []byte(s.opts.Token)) == 1
}

type panelConfig struct {
	Name      string `json:"name"`
	IntervalS int    `json:"interval_s"`
}

func (s *Server) config(w http.ResponseWriter, _ *http.Request) {
	out := struct {
		Profiles []string      `json:"profiles"`
		Panels   []panelConfig `json:"panels"`
	}{Profiles: s.opts.Profiles}
	for _, p := range s.opts.Panels {
		out.Panels = append(out.Panels, panelConfig{Name: p.Name, IntervalS: int(p.Interval / time.Second)})
	}
	writeJSON(w, out)
}

func (s *Server) panel(refresh bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profile, name := r.PathValue("profile"), r.PathValue("panel")
		p, ok := s.panels[name]
		if !ok || !s.profiles[profile] {
			http.NotFound(w, r)
			return
		}
		e, err := s.get(r.Context(), profile, p, refresh)
		if err != nil {
			// The page went away while waiting.
			return
		}
		writeJSON(w, e)
	}
}

// get returns the panel, fetching it first when it is due: when it was never
// fetched, when its interval has passed, or on a refresh at least
// refreshGap after the last fetch. Requests that arrive together share one
// fetch, which goes on when the request that started it goes away.
func (s *Server) get(ctx context.Context, profile string, p Panel, refresh bool) (entry, error) {
	key := profile + "/" + p.Name
	s.mu.Lock()
	e, ok := s.entries[key]
	if !ok {
		e = &entry{IntervalS: int(p.Interval / time.Second)}
		s.entries[key] = e
	}
	wait := e.fetching || s.due(e, p, refresh)
	s.mu.Unlock()

	if wait {
		// Joins the fetch under way, or starts one if it is still due.
		ch := s.fetches.DoChan(key, func() (any, error) {
			s.mu.Lock()
			due := s.due(e, p, refresh)
			s.mu.Unlock()
			if due {
				s.fetch(profile, p, e)
			}
			return nil, nil
		})
		select {
		case <-ch:
		case <-ctx.Done():
			return entry{}, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return *e, nil
}

// due reports whether e needs a fetch. s.mu must be held.
func (s *Server) due(e *entry, p Panel, refresh bool) bool {
	age := s.now().Sub(e.tried)
	// The page polls at the same interval, so a little slack keeps it from
	// getting the old data every other time.
	return e.tried.IsZero() || age >= p.Interval*4/5 || (refresh && age >= refreshGap)
}

func (s *Server) fetch(profile string, p Panel, e *entry) {
	start := s.now()
	s.mu.Lock()
	e.tried, e.fetching = start, true
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(s.ctx, p.Timeout)
	defer cancel()
	data, err := s.opts.Fetch(ctx, profile, p.Name)

	s.mu.Lock()
	defer s.mu.Unlock()
	e.fetching = false
	e.DurationMS = s.now().Sub(start).Milliseconds()
	if err != nil {
		e.Error = err.Error()
		return
	}
	e.Data, e.Error = data, ""
	e.UpdatedAt = &start
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
