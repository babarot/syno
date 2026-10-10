package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/babarot/syno/internal/config"
	"github.com/babarot/syno/internal/dashboard"
	"github.com/babarot/syno/internal/doctor"
	"github.com/babarot/syno/internal/dsm"
)

func newDashboardCmd() *cobra.Command {
	var (
		port   int
		noOpen bool
	)

	c := &cobra.Command{
		Use:     "dashboard",
		Aliases: []string{"dash"},
		Short:   "Show the NAS on a web page served on this machine",
		Long: `Serve a web page on 127.0.0.1 that shows the health checks, CPU and memory,
storage, disks, shared folders, recycle bins, containers and updates of the
NAS, each refreshed at its own pace.
The page only reads; it changes nothing on the NAS.

The page covers every profile, or only the one given by --profile or
SYNO_PROFILE. The NAS is asked only while a page is open, and one session
per profile is kept for the page.

The URL carries a token, which the page trades for a cookie: a user or program
on this machine that has not seen the URL cannot read the page. Stop with
Ctrl-C.`,
		Example: `  syno dashboard
  syno dash --port 8765 --no-open`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDashboard(cmd.Context(), port, !noOpen)
		},
	}

	c.Flags().IntVar(&port, "port", 0, "Port to listen on (default: any free port)")
	c.Flags().BoolVar(&noOpen, "no-open", false, "Print the URL without opening a browser")

	return c
}

// dashboardPanels are the panels of the page. A panel's interval is how
// old its data may get before a page asking for it makes the server fetch
// it again.
var dashboardPanels = []dashboard.Panel{
	{Name: "system", Interval: 5 * time.Second, Timeout: 10 * time.Second},
	{Name: "info", Interval: time.Minute, Timeout: 15 * time.Second},
	{Name: "storage", Interval: time.Minute, Timeout: 30 * time.Second},
	{Name: "containers", Interval: 30 * time.Second, Timeout: 30 * time.Second},
	{Name: "doctor", Interval: 5 * time.Minute, Timeout: time.Minute},
	{Name: "shares", Interval: 5 * time.Minute, Timeout: 30 * time.Second},
	// Summing the recycle bins makes the NAS walk their files, so it is
	// done when the page opens, then hourly or on the page's button.
	{Name: "recycle", Interval: time.Hour, Timeout: recycleTimeout + 30*time.Second},
	{Name: "updates", Interval: time.Hour, Timeout: time.Minute},
}

// updateChecks are the checks that make the NAS ask Synology's servers. The
// updates panel runs them hourly instead of the doctor panel.
var updateChecks = []string{"dsm-update", "package-update"}

func runDashboard(ctx context.Context, port int, open bool) error {
	profiles, err := dashboardProfiles()
	if err != nil {
		return err
	}
	base, err := doctorOptions(nil, nil)
	if err != nil {
		return err
	}
	cfg := newPanelConfig(base)

	src := newDashboardSource(profiles, func(ctx context.Context, c *dsm.Client, panel string) (any, error) {
		return readPanel(ctx, c, panel, cfg)
	})
	defer src.close()

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	slices.Sort(names)
	srv, err := dashboard.New(ctx, dashboard.Options{
		Addr:     ln.Addr().String(),
		Profiles: names,
		Panels:   dashboardPanels,
		Fetch:    src.fetch,
	})
	if err != nil {
		_ = ln.Close()
		return err
	}

	fmt.Fprintf(os.Stderr, "Dashboard: %s\nStop with Ctrl-C.\n", srv.URL())
	if open {
		if err := openBrowser(srv.URL()); err != nil {
			fmt.Fprintf(os.Stderr, "Could not open a browser (%v): open the URL above.\n", err)
		}
	}
	return srv.Serve(ctx, ln)
}

// dashboardProfiles returns the profile given by --profile or SYNO_PROFILE,
// or else every profile.
func dashboardProfiles() (map[string]*config.Profile, error) {
	cfg, err := loadProfiles()
	if err != nil {
		return nil, err
	}
	if name := profileName(); name != "" {
		name, p, err := cfg.Select(name)
		if err != nil {
			return nil, err
		}
		return map[string]*config.Profile{name: p}, nil
	}
	if len(cfg.Profiles) == 0 {
		return nil, config.ErrNoProfile
	}
	return cfg.Profiles, nil
}

// dashboardSource fetches the panels with one session per profile.
type dashboardSource struct {
	profiles map[string]*config.Profile
	read     func(ctx context.Context, c *dsm.Client, panel string) (any, error)

	mu       sync.Mutex
	sessions map[string]*liveSession
}

func newDashboardSource(profiles map[string]*config.Profile, read func(context.Context, *dsm.Client, string) (any, error)) *dashboardSource {
	return &dashboardSource{profiles: profiles, read: read, sessions: map[string]*liveSession{}}
}

func (s *dashboardSource) fetch(ctx context.Context, profile, panel string) (any, error) {
	p, ok := s.profiles[profile]
	if !ok {
		return nil, fmt.Errorf("no profile named %q", profile)
	}
	s.mu.Lock()
	sess, ok := s.sessions[profile]
	if !ok {
		sess = newLiveSession(p, keyringStore{})
		s.sessions[profile] = sess
	}
	s.mu.Unlock()

	var out any
	err := sess.do(ctx, func(c *dsm.Client) (err error) {
		out, err = s.read(ctx, c, panel)
		return err
	})
	return out, err
}

// close ends the sessions that could not be saved.
func (s *dashboardSource) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		sess.close()
	}
}

// The JSON of the panels. The page shows no serial number.
type (
	infoPanel struct {
		Host               string  `json:"host"`
		Model              string  `json:"model"`
		DSM                string  `json:"dsm"`
		Uptime             string  `json:"uptime"`
		CPUCores           string  `json:"cpu_cores,omitempty"`
		TemperatureC       float64 `json:"temperature_c"`
		TemperatureWarning bool    `json:"temperature_warning"`
	}
	systemPanel struct {
		CPUPercent    float64 `json:"cpu_percent"`
		MemoryPercent float64 `json:"memory_percent"`
		MemoryBytes   float64 `json:"memory_bytes"`
		// Volumes is the I/O of each volume, which the storage panel draws.
		Volumes []volumeIO `json:"volumes"`
	}
	volumeIO struct {
		Path             string  `json:"path"`
		ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
		WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
		BusyPercent      float64 `json:"busy_percent"`
	}
	storagePanel struct {
		Pools      []dashboardPool `json:"pools"`
		Volumes    []volumeReport  `json:"volumes"`
		Disks      []dashboardDisk `json:"disks"`
		Bays       *bayReport      `json:"bays,omitempty"`
		Thresholds panelThresholds `json:"thresholds"`
	}
	// dashboardPool adds the name Storage Manager gives the RAID type.
	dashboardPool struct {
		poolReport
		RAID string `json:"raid"`
	}
	// dashboardDisk adds what the disk cards show to the disk of status.
	dashboardDisk struct {
		diskReport
		Type       string  `json:"type"`
		BadSectors float64 `json:"bad_sectors"`
		// Bay is the bay of the NAS the disk is in, or 0 for an M.2 SSD or
		// a disk of an expansion unit.
		Bay int `json:"bay,omitempty"`
	}
	// panelThresholds are the doctor thresholds the page colors with.
	panelThresholds struct {
		ScrubAgeDays float64 `json:"scrub_age_days"`
		VolumeWarn   float64 `json:"volume_warn"`
		VolumeFail   float64 `json:"volume_fail"`
		DiskTempWarn float64 `json:"disk_temp_warn"`
		DiskTempFail float64 `json:"disk_temp_fail"`
	}
	containersPanel struct {
		Installed bool `json:"installed"`
		*containerList
	}
	// updatesPanel has every package, to count the ones up to date, and the
	// results of updateChecks, which the page adds to the health checks.
	updatesPanel struct {
		Host     string          `json:"host"`
		Packages []packageRow    `json:"packages"`
		Checks   []doctor.Result `json:"checks"`
	}
)

// panelConfig is how the panels run the checks.
type panelConfig struct {
	// doctor runs every check but updateChecks, and updates only those of
	// updateChecks that config.yaml does not skip.
	doctor, updates doctor.Options
	// hidden are the checks the doctor panel skips only because the
	// updates panel runs them, so they are not shown as skipped.
	hidden []string
}

func newPanelConfig(base doctor.Options) panelConfig {
	cfg := panelConfig{doctor: base, updates: base}
	cfg.doctor.Skip = append(slices.Clone(base.Skip), updateChecks...)
	cfg.updates.Skip = nil
	cfg.updates.Only = nil
	for _, name := range updateChecks {
		if !slices.Contains(base.Skip, name) {
			cfg.updates.Only = append(cfg.updates.Only, name)
			cfg.hidden = append(cfg.hidden, name)
		}
	}
	return cfg
}

// readPanel fetches one panel.
func readPanel(ctx context.Context, c *dsm.Client, panel string, cfg panelConfig) (any, error) {
	th := cfg.doctor.Thresholds
	switch panel {
	case "system":
		u, err := c.Utilization(ctx)
		if err != nil {
			return nil, err
		}
		out := systemPanel{
			CPUPercent:    float64(u.CPU.UserLoad + u.CPU.SystemLoad + u.CPU.OtherLoad),
			MemoryPercent: float64(u.Memory.RealUsage),
			MemoryBytes:   float64(u.Memory.TotalReal) * 1024,
			Volumes:       make([]volumeIO, 0, len(u.Space.Volume)),
		}
		for _, v := range u.Space.Volume {
			out.Volumes = append(out.Volumes, volumeIO{
				Path: "/" + v.DisplayName, ReadBytesPerSec: float64(v.ReadBytes),
				WriteBytesPerSec: float64(v.WriteBytes), BusyPercent: float64(v.Utilization),
			})
		}
		return out, nil

	case "info":
		s, err := c.SystemInfo(ctx)
		if err != nil {
			return nil, err
		}
		return infoPanel{
			Host: c.Base, Model: s.Model, DSM: s.FirmwareVer, Uptime: formatUptime(s.UpTime), CPUCores: s.CPUCores,
			TemperatureC: float64(s.SysTemp), TemperatureWarning: s.SysTempWarn || s.TemperatureWarning,
		}, nil

	case "storage":
		st, err := c.Storage(ctx)
		if err != nil {
			return nil, err
		}
		// The I/O comes with the system panel, which is refreshed more often.
		pools, volumes, disks := storageReports(st, powerOnHours(ctx, c, st.Disks), nil)
		out := storagePanel{
			Pools: make([]dashboardPool, len(pools)), Volumes: volumes, Disks: make([]dashboardDisk, len(disks)), Bays: newBayReport(st),
			Thresholds: panelThresholds{th.ScrubAgeWarn.Hours() / 24, th.VolumeUsageWarn, th.VolumeUsageFail, th.DiskTempWarn, th.DiskTempFail},
		}
		for i, p := range pools {
			out.Pools[i] = dashboardPool{poolReport: p, RAID: dsm.RAIDName(p.Type)}
		}
		for i, d := range disks {
			sd := st.Disks[i]
			typ := "HDD"
			switch {
			case sd.IsM2():
				typ = "M.2"
			case sd.IsSSD:
				typ = "SSD"
			}
			out.Disks[i] = dashboardDisk{diskReport: d, Type: typ, BadSectors: float64(sd.Unc)}
			if sd.Container.Type == "internal" && !sd.IsM2() {
				out.Disks[i].Bay = sd.SlotID
			}
		}
		return out, nil

	case "containers":
		l, err := readContainers(ctx, c, containerListOptions{Usage: true})
		if errors.Is(err, errNoContainerManager) {
			return containersPanel{containerList: &containerList{Host: c.Base, Containers: []containerRow{}}}, nil
		}
		if err != nil {
			return nil, err
		}
		return containersPanel{Installed: true, containerList: l}, nil

	case "doctor":
		opts := cfg.doctor
		opts.Now = time.Now()
		r := readDoctor(ctx, c, opts)
		r.Results = slices.DeleteFunc(r.Results, func(x doctor.Result) bool {
			return x.Level == doctor.Skip && slices.Contains(cfg.hidden, x.Check)
		})
		return r, nil

	case "shares", "recycle":
		return readShares(ctx, c, panel == "recycle")

	case "updates":
		l, err := readPackages(ctx, c, false)
		if err != nil {
			return nil, err
		}
		out := updatesPanel{Host: l.Host, Packages: l.Packages, Checks: []doctor.Result{}}
		// Only with nothing in it would run every check.
		if len(cfg.updates.Only) > 0 {
			opts := cfg.updates
			opts.Now = time.Now()
			out.Checks = readDoctor(ctx, c, opts).Results
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown panel %q", panel)
}

// openBrowser opens url in the default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
