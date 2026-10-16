// Copyright 2010 Rebel Media
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Command jellyfin_exporter is a Prometheus exporter for a Jellyfin media
// server. It serves a /metrics endpoint that, on each scrape, queries the
// Jellyfin API through the registered collectors (see the collector package)
// and returns the results. Configuration is via command-line flags and
// environment variables; run with --help for the full list.
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/user"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/prometheus/common/promslog"

	"github.com/alecthomas/kong"
	"github.com/prometheus/client_golang/prometheus"
	promcollectors "github.com/prometheus/client_golang/prometheus/collectors"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"

	"github.com/rebelcore/jellyfin_exporter/collector"
	"github.com/rebelcore/jellyfin_exporter/config"
)

// handler builds and serves the Prometheus metrics endpoint. It keeps an
// "unfiltered" handler covering all enabled collectors and builds filtered
// handlers on demand when a request narrows the set via collect[]/exclude[].
type handler struct {
	unfilteredHandler       http.Handler
	enabledCollectors       []string
	enabledCollectorsOnce   sync.Once
	exporterMetricsRegistry *prometheus.Registry
	includeExporterMetrics  bool
	maxRequests             int
	logger                  *slog.Logger
}

// These package-level vars indirect over external constructors so tests can
// substitute them; production code uses the real implementations assigned here.
var (
	listenAndServe       = web.ListenAndServe
	currentUser          = user.Current
	newLandingPage       = web.NewLandingPage
	newJellyfinCollector = collector.NewJellyfinCollector
	registerWithRegistry = func(r *prometheus.Registry, c prometheus.Collector) error { return r.Register(c) }
)

// newHandler constructs the metrics handler, optionally registering the
// exporter's own process and Go-runtime metrics, and pre-builds the unfiltered
// collector handler.
func newHandler(includeExporterMetrics bool, maxRequests int, logger *slog.Logger) (*handler, error) {
	h := &handler{
		exporterMetricsRegistry: prometheus.NewRegistry(),
		includeExporterMetrics:  includeExporterMetrics,
		maxRequests:             maxRequests,
		logger:                  logger,
	}
	if h.includeExporterMetrics {
		h.exporterMetricsRegistry.MustRegister(
			promcollectors.NewProcessCollector(promcollectors.ProcessCollectorOpts{}),
			promcollectors.NewGoCollector(),
		)
	}
	innerHandler, err := h.innerHandler()
	if err != nil {
		return nil, err
	}
	h.unfilteredHandler = innerHandler
	return h, nil
}

// ServeHTTP serves the metrics endpoint. With no collect[]/exclude[] query
// parameters it uses the pre-built unfiltered handler; otherwise it builds a
// handler for just the requested collectors. Combining collect[] and exclude[]
// in one request is rejected.
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	collects := r.URL.Query()["collect[]"]
	h.logger.Debug("collect query:", "collects", collects)

	excludes := r.URL.Query()["exclude[]"]
	h.logger.Debug("exclude query:", "excludes", excludes)

	if len(collects) == 0 && len(excludes) == 0 {
		h.unfilteredHandler.ServeHTTP(w, r)
		return
	}

	if len(collects) > 0 && len(excludes) > 0 {
		h.logger.Debug("rejecting combined collect and exclude queries")
		http.Error(w, "Combined collect and exclude queries are not allowed.", http.StatusBadRequest)
		return
	}

	// collect[] names the collectors to run directly; exclude[] is the inverse,
	// so convert it into a keep-list of every enabled collector except those
	// named.
	filters := collects
	if len(excludes) > 0 {
		// A name that matches no enabled collector cannot narrow anything, so
		// warn rather than silently serving the full set for a typo. It stays
		// non-fatal because excluding an already-disabled collector is a
		// legitimate no-op.
		for _, e := range excludes {
			if !slices.Contains(h.enabledCollectors, e) {
				h.logger.Warn("Ignoring unknown or disabled collector in exclude[]", "collector", e)
			}
		}

		filters = nil
		for _, c := range h.enabledCollectors {
			if !slices.Contains(excludes, c) {
				filters = append(filters, c)
			}
		}

		// Excluding every enabled collector leaves nothing to collect. That must
		// not fall through to innerHandler, where an empty filter list means
		// "every enabled collector" — the exact inverse of what was asked.
		if len(filters) == 0 {
			h.logger.Debug("rejecting exclude query that removes every collector")
			http.Error(w, "All enabled collectors have been excluded.", http.StatusBadRequest)
			return
		}
	}

	filteredHandler, err := h.innerHandler(filters...)
	if err != nil {
		h.logger.Warn("Couldn't create filtered metrics handler:", "err", err)
		http.Error(w, fmt.Sprintf("Couldn't create filtered metrics handler: %s", err), http.StatusBadRequest)
		return
	}
	filteredHandler.ServeHTTP(w, r)
}

// innerHandler builds a Prometheus HTTP handler for the given collector filters
// (all enabled collectors when none are passed), registering the build-version
// collector and the Jellyfin collector against a fresh registry.
func (h *handler) innerHandler(filters ...string) (http.Handler, error) {
	nc, err := newJellyfinCollector(h.logger, filters...)
	if err != nil {
		return nil, fmt.Errorf("couldn't create collector: %s", err)
	}

	// Populate the full set of enabled collectors exactly once. This runs at
	// startup (newHandler calls innerHandler with no filters). Guarding it with
	// a sync.Once prevents concurrent ServeHTTP requests that resolve to an
	// empty filter set (e.g. excluding every collector) from racing on, and
	// duplicating entries in, h.enabledCollectors.
	if len(filters) == 0 {
		h.enabledCollectorsOnce.Do(func() {
			h.logger.Info("Enabled collectors")
			for n := range nc.Collectors {
				h.enabledCollectors = append(h.enabledCollectors, n)
			}
			slices.Sort(h.enabledCollectors)
			for _, c := range h.enabledCollectors {
				h.logger.Info(c)
			}
		})
	}

	r := prometheus.NewRegistry()
	r.MustRegister(versioncollector.NewCollector("jellyfin_exporter"))
	if err := registerWithRegistry(r, nc); err != nil {
		return nil, fmt.Errorf("couldn't register jellyfin collector: %s", err)
	}

	// With exporter self-metrics enabled, gather from both the exporter registry
	// and the Jellyfin registry, and wrap the handler so the scrape's own request
	// is counted. Otherwise serve only the Jellyfin metrics.
	var handler http.Handler
	if h.includeExporterMetrics {
		handler = promhttp.HandlerFor(
			prometheus.Gatherers{h.exporterMetricsRegistry, r},
			promhttp.HandlerOpts{
				ErrorLog:            slog.NewLogLogger(h.logger.Handler(), slog.LevelError),
				ErrorHandling:       promhttp.ContinueOnError,
				MaxRequestsInFlight: h.maxRequests,
				Registry:            h.exporterMetricsRegistry,
			},
		)
		handler = promhttp.InstrumentMetricHandler(
			h.exporterMetricsRegistry, handler,
		)
	} else {
		handler = promhttp.HandlerFor(
			r,
			promhttp.HandlerOpts{
				ErrorLog:            slog.NewLogLogger(h.logger.Handler(), slog.LevelError),
				ErrorHandling:       promhttp.ContinueOnError,
				MaxRequestsInFlight: h.maxRequests,
			},
		)
	}

	return handler, nil
}

// pprofPrefix is the route net/http/pprof is mounted under when profiling is
// enabled; the metrics path must not collide with it.
const pprofPrefix = "/debug/pprof/"

// validateMetricsPath rejects --web.telemetry-path values that http.ServeMux
// would panic on: it must be a plain absolute path, with no whitespace and no
// wildcard syntax, and it must not collide with the pprof routes.
func validateMetricsPath(metricsPath string, enablePprof bool) error {
	if metricsPath == "" {
		return errors.New("web.telemetry-path must not be empty")
	}
	if !strings.HasPrefix(metricsPath, "/") {
		return fmt.Errorf("web.telemetry-path %q must start with %q", metricsPath, "/")
	}
	if strings.ContainsAny(metricsPath, " \t\r\n") {
		return fmt.Errorf("web.telemetry-path %q must not contain whitespace", metricsPath)
	}
	if strings.ContainsAny(metricsPath, "{}") {
		return fmt.Errorf("web.telemetry-path %q must not contain wildcard syntax", metricsPath)
	}
	if enablePprof && strings.HasPrefix(metricsPath, pprofPrefix) {
		return fmt.Errorf("web.telemetry-path %q conflicts with the pprof endpoints at %q", metricsPath, pprofPrefix)
	}
	return nil
}

// buildMux assembles the HTTP routes: the metrics handler at metricsPath, the
// pprof endpoints when enablePprof is set, and a landing page at "/" (unless
// metrics are already served there). The landing page advertises the pprof
// links only when profiling is enabled, so they never point at a 404.
func buildMux(metricsPath string, metricsHandler http.Handler, enablePprof bool) (*http.ServeMux, error) {
	// http.ServeMux panics on a malformed pattern, so validate the operator's
	// value first and return an error instead of crashing at startup with a
	// stack trace.
	if err := validateMetricsPath(metricsPath, enablePprof); err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle(metricsPath, metricsHandler)

	// Profiling endpoints, registered only when enabled — the same routes
	// net/http/pprof installs on the default mux.
	if enablePprof {
		mux.HandleFunc(pprofPrefix, pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	if metricsPath != "/" {
		landingConfig := web.LandingConfig{
			Name:        "Jellyfin Exporter",
			Description: "Prometheus Jellyfin Exporter",
			Version:     version.Info(),
			// Only advertise the pprof links when the endpoints are actually
			// registered; otherwise the toolkit defaults Profiling to "true"
			// and the landing page shows links that 404.
			Profiling: strconv.FormatBool(enablePprof),
			Links: []web.LandingLinks{
				{
					Address: metricsPath,
					Text:    "Metrics",
				},
			},
		}
		landingPage, err := newLandingPage(landingConfig)
		if err != nil {
			return nil, err
		}
		mux.Handle("/", landingPage)
	}

	return mux, nil
}

// run wires together configuration, the metrics handler and the HTTP mux, then
// serves until the listener stops. It is the testable core of main: every input
// is a parameter and the listener is the swappable listenAndServe.
func run(
	metricsPath string,
	disableExporterMetrics bool,
	maxRequests int,
	disableDefaultCollectors bool,
	maxProcs int,
	enablePprof bool,
	toolkitFlags *web.FlagConfig,
	logger *slog.Logger,
) error {
	if toolkitFlags == nil {
		return errors.New("missing web flags config")
	}

	if disableDefaultCollectors {
		collector.DisableDefaultCollectors()
	}
	logger.Info("Starting jellyfin_exporter", "version", version.Info(), "git_tag", gitTag())
	logger.Info("Build context", "build_context", version.BuildContext())

	if u, err := currentUser(); err == nil && u.Uid == "0" {
		logger.Warn("Jellyfin Exporter is running as root user. This exporter is designed to run as unprivileged user, root is not required.")
	}

	runtime.GOMAXPROCS(maxProcs)
	logger.Debug("Go MAXPROCS", "procs", runtime.GOMAXPROCS(0))

	metricsHandler, err := newHandler(!disableExporterMetrics, maxRequests, logger)
	if err != nil {
		return fmt.Errorf("couldn't create metrics handler: %w", err)
	}

	mux, err := buildMux(metricsPath, metricsHandler, enablePprof)
	if err != nil {
		return fmt.Errorf("couldn't create HTTP mux: %w", err)
	}

	server := &http.Server{Handler: mux}
	return listenAndServe(server, toolkitFlags, logger)
}

// flags is the exporter's command line, parsed by kong. The Jellyfin connection
// and collector flags are defined by the packages that use them; newParser
// embeds them alongside these.
type flags struct {
	MetricsPath              string           `name:"web.telemetry-path" default:"/metrics" help:"Path under which to expose metrics."`
	DisableExporterMetrics   bool             `name:"web.disable-exporter-metrics" negatable:"" help:"Exclude metrics about the exporter itself (promhttp_*, process_*, go_*)."`
	MaxRequests              int              `name:"web.max-requests" default:"40" help:"Maximum number of parallel scrape requests. Use 0 to disable."`
	DisableDefaultCollectors bool             `name:"collector.disable-defaults" negatable:"" help:"Set all collectors to disabled by default."`
	MaxProcs                 int              `name:"runtime.gomaxprocs" env:"GOMAXPROCS" default:"1" help:"The target number of CPUs Go will run on (GOMAXPROCS)"`
	EnablePprof              bool             `name:"web.enable-pprof" negatable:"" help:"Enable pprof profiling endpoints under /debug/pprof/."`
	WebListenAddresses       []string         `name:"web.listen-address" default:":9594" sep:"none" placeholder:":9594" help:"Addresses on which to expose metrics and web interface. Repeatable for multiple addresses. Examples: :9100 or [::1]:9100 for http, vsock://:9100 for vsock"`
	WebConfigFile            string           `name:"web.config.file" help:"Path to configuration file that can enable TLS or authentication. See: https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md"`
	LogLevel                 string           `name:"log.level" default:"info" enum:"debug,info,warn,error" help:"Only log messages with the given severity or above. One of: [debug, info, warn, error]"`
	LogFormat                string           `name:"log.format" default:"logfmt" enum:"logfmt,json" help:"Output format of log messages. One of: [logfmt, json]"`
	Version                  kong.VersionFlag `help:"Show application version."`

	// Systemd is embedded on Linux only (see newParser), the one platform
	// where exporter-toolkit supports socket activation.
	Systemd systemdFlags `kong:"-"`
}

// systemdFlags holds the Linux-only --web.systemd-socket flag.
type systemdFlags struct {
	WebSystemdSocket bool `name:"web.systemd-socket" negatable:"" help:"Use systemd socket activation listeners instead of port listeners (Linux only)."`
}

// newParser builds the kong parser that fills f from the command line. The
// Jellyfin connection and collector flags are embedded alongside f's own, as is
// --web.systemd-socket on Linux.
func newParser(f *flags, options ...kong.Option) *kong.Kong {
	options = append(options,
		kong.Vars{"version": versionString("jellyfin_exporter")},
		kong.Embed(config.Flags()),
	)
	for _, group := range collector.Flags() {
		options = append(options, kong.Embed(group))
	}
	if runtime.GOOS == "linux" {
		options = append(options, kong.Embed(&f.Systemd))
	}
	return kong.Must(f, options...)
}

// parseFlags parses args with parser, then records which collectors were
// toggled explicitly, for --collector.disable-defaults.
//
// An environment variable that is set but empty counts as unset, so its flag
// keeps the default. kong would otherwise parse the empty value, leaving an
// empty --jellyfin.address, satisfying the required --jellyfin.token with an
// empty token, or failing on an empty GOMAXPROCS.
func parseFlags(parser *kong.Kong, args []string) error {
	for _, group := range parser.Model.AllFlags(false) {
		for _, flag := range group {
			for _, name := range flag.Envs {
				if value, ok := os.LookupEnv(name); ok && value == "" {
					if err := os.Unsetenv(name); err != nil {
						return err
					}
				}
			}
		}
	}

	ctx, err := parser.Parse(args)
	if err != nil {
		return err
	}
	collector.MarkForcedCollectors(ctx)
	return nil
}

// logConfig turns --log.level and --log.format into the promslog
// configuration.
func (f *flags) logConfig() (*promslog.Config, error) {
	cfg := &promslog.Config{Level: promslog.NewLevel(), Format: promslog.NewFormat()}
	if err := cfg.Level.Set(f.LogLevel); err != nil {
		return nil, err
	}
	if err := cfg.Format.Set(f.LogFormat); err != nil {
		return nil, err
	}
	return cfg, nil
}

// webFlags hands the listener flags to exporter-toolkit.
func (f *flags) webFlags() *web.FlagConfig {
	return &web.FlagConfig{
		WebListenAddresses: &f.WebListenAddresses,
		WebSystemdSocket:   &f.Systemd.WebSystemdSocket,
		WebConfigFile:      &f.WebConfigFile,
	}
}

// main parses flag and environment configuration, then hands off to run,
// exiting non-zero on error.
func main() {
	var f flags
	parser := newParser(&f)
	if err := parseFlags(parser, os.Args[1:]); err != nil {
		// Exit 1 on a usage error, like any other startup failure, rather than
		// with kong's status 80.
		parser.Errorf("%s", err)
		os.Exit(1)
	}
	logConfig, err := f.logConfig()
	parser.FatalIfErrorf(err)
	logger := promslog.New(logConfig)

	if err := run(
		f.MetricsPath,
		f.DisableExporterMetrics,
		f.MaxRequests,
		f.DisableDefaultCollectors,
		f.MaxProcs,
		f.EnablePprof,
		f.webFlags(),
		logger,
	); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

// versionString renders the multi-line --version output, combining the build
// metadata from the version package with the derived git tag.
func versionString(program string) string {
	return fmt.Sprintf(`%s, version %s (branch: %s, revision: %s)
  build user:       %s
  build date:       %s
  go version:       %s
  platform:         %s/%s
  tags:             git=%s, go=%s`,
		program,
		version.Version,
		version.Branch,
		version.GetRevision(),
		version.BuildUser,
		version.BuildDate,
		version.GoVersion,
		version.GoOS,
		version.GoArch,
		gitTag(),
		version.GetTags(),
	)
}

// gitTag normalises the build version into a "v"-prefixed tag, returning
// "unknown" when no version was stamped into the binary.
func gitTag() string {
	v := strings.TrimSpace(version.Version)
	if v == "" || v == "unknown" {
		return "unknown"
	}
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}
