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

package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/user"
	"runtime"
	"slices"
	"sort"
	"strings"

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

type handler struct {
	unfilteredHandler       http.Handler
	enabledCollectors       []string
	exporterMetricsRegistry *prometheus.Registry
	includeExporterMetrics  bool
	maxRequests             int
	logger                  *slog.Logger
}

var (
	listenAndServe       = web.ListenAndServe
	currentUser          = user.Current
	newLandingPage       = web.NewLandingPage
	newJellyfinCollector = collector.NewJellyfinCollector
	registerWithRegistry = func(r *prometheus.Registry, c prometheus.Collector) error { return r.Register(c) }
)

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

	filters := &collects
	if len(excludes) > 0 {
		f := []string{}
		for _, c := range h.enabledCollectors {
			if (slices.Index(excludes, c)) == -1 {
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

func (h *handler) innerHandler(filters ...string) (http.Handler, error) {
	nc, err := newJellyfinCollector(h.logger, filters...)
	if err != nil {
		return nil, fmt.Errorf("couldn't create collector: %s", err)
	}

	if len(filters) == 0 {
		h.logger.Info("Enabled collectors")
		for n := range nc.Collectors {
			h.enabledCollectors = append(h.enabledCollectors, n)
		}
		sort.Strings(h.enabledCollectors)
		for _, c := range h.enabledCollectors {
			h.logger.Info(c)
		}
	}

	r := prometheus.NewRegistry()
	r.MustRegister(versioncollector.NewCollector("jellyfin_exporter"))
	if err := registerWithRegistry(r, nc); err != nil {
		return nil, fmt.Errorf("couldn't register jellyfin collector: %s", err)
	}

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

func buildMux(metricsPath string, metricsHandler http.Handler) (*http.ServeMux, error) {
	mux := http.NewServeMux()
	mux.Handle(metricsPath, metricsHandler)

	if metricsPath != "/" {
		landingConfig := web.LandingConfig{
			Name:        "Jellyfin Exporter",
			Description: "Prometheus Jellyfin Exporter",
			Version:     version.Info(),
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

func run(
	metricsPath string,
	disableExporterMetrics bool,
	maxRequests int,
	disableDefaultCollectors bool,
	maxProcs int,
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

	mux, err := buildMux(metricsPath, metricsHandler)
	if err != nil {
		return fmt.Errorf("couldn't create HTTP mux: %w", err)
	}

	server := &http.Server{Handler: mux}
	return listenAndServe(server, toolkitFlags, logger)
}

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
		toolkitFlags,
		logger,
	); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

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
