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
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/prometheus/common/promslog"
	"github.com/prometheus/common/promslog/flag"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	promcollectors "github.com/prometheus/client_golang/prometheus/collectors"
	versioncollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/prometheus/exporter-toolkit/web/kingpinflag"

	"github.com/rebelcore/jellyfin_exporter/collector"
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
	filters := &collects
	if len(excludes) > 0 {
		f := []string{}
		for _, c := range h.enabledCollectors {
			if !slices.Contains(excludes, c) {
				f = append(f, c)
			}
		}
		filters = &f
	}

	filteredHandler, err := h.innerHandler(*filters...)
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
			sort.Strings(h.enabledCollectors)
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

// buildMux assembles the HTTP routes: the metrics handler at metricsPath, the
// pprof endpoints when enablePprof is set, and a landing page at "/" (unless
// metrics are already served there). The landing page advertises the pprof
// links only when profiling is enabled, so they never point at a 404.
func buildMux(metricsPath string, metricsHandler http.Handler, enablePprof bool) (*http.ServeMux, error) {
	mux := http.NewServeMux()
	mux.Handle(metricsPath, metricsHandler)

	// Profiling endpoints, registered only when enabled — the same routes
	// net/http/pprof installs on the default mux.
	if enablePprof {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
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

// main parses flag and environment configuration, then hands off to run,
// exiting non-zero on error.
func main() {
	var (
		metricsPath = kingpin.Flag(
			"web.telemetry-path",
			"Path under which to expose metrics.",
		).Default("/metrics").String()
		disableExporterMetrics = kingpin.Flag(
			"web.disable-exporter-metrics",
			"Exclude metrics about the exporter itself (promhttp_*, process_*, go_*).",
		).Bool()
		maxRequests = kingpin.Flag(
			"web.max-requests",
			"Maximum number of parallel scrape requests. Use 0 to disable.",
		).Default("40").Int()
		disableDefaultCollectors = kingpin.Flag(
			"collector.disable-defaults",
			"Set all collectors to disabled by default.",
		).Default("false").Bool()
		maxProcs = kingpin.Flag(
			"runtime.gomaxprocs", "The target number of CPUs Go will run on (GOMAXPROCS)",
		).Envar("GOMAXPROCS").Default("1").Int()
		enablePprof = kingpin.Flag(
			"web.enable-pprof",
			"Enable pprof profiling endpoints under /debug/pprof/.",
		).Default("false").Bool()
		toolkitFlags = kingpinflag.AddFlags(kingpin.CommandLine, ":9594")
	)

	promslogConfig := &promslog.Config{}
	flag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.Version(versionString("jellyfin_exporter"))
	kingpin.CommandLine.UsageWriter(os.Stdout)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promslog.New(promslogConfig)

	if err := run(
		*metricsPath,
		*disableExporterMetrics,
		*maxRequests,
		*disableDefaultCollectors,
		*maxProcs,
		*enablePprof,
		toolkitFlags,
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
