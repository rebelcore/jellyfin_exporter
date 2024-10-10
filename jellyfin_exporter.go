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
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/user"
	"runtime"
	"slices"
	"sort"

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

func newHandler(includeExporterMetrics bool, maxRequests int, logger *slog.Logger) *handler {
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
	if innerHandler, err := h.innerHandler(); err != nil {
		panic(fmt.Sprintf("Couldn't create metrics handler: %s", err))
	} else {
		h.unfilteredHandler = innerHandler
	}
	return h
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
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Combined collect and exclude queries are not allowed."))
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
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Couldn't create filtered metrics handler: %s", err)))
		return
	}
	filteredHandler.ServeHTTP(w, r)
}

func (h *handler) innerHandler(filters ...string) (http.Handler, error) {
	nc, err := collector.NewJellyfinCollector(h.logger, filters...)
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
	if err := r.Register(nc); err != nil {
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
	kingpin.Version(version.Print("jellyfin_exporter"))
	kingpin.CommandLine.UsageWriter(os.Stdout)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promslog.New(promslogConfig)

	if *disableDefaultCollectors {
		collector.DisableDefaultCollectors()
	}
	logger.Info("Starting jellyfin_exporter", "version", version.Info())
	logger.Info("Build context", "build_context", version.BuildContext())
	if user, err := user.Current(); err == nil && user.Uid == "0" {
		logger.Warn("Jellyfin Exporter is running as root user. This exporter is designed to run as unprivileged user, root is not required.")
	}
	runtime.GOMAXPROCS(*maxProcs)
	logger.Debug("Go MAXPROCS", "procs", runtime.GOMAXPROCS(0))

	http.Handle(*metricsPath, newHandler(!*disableExporterMetrics, *maxRequests, logger))
	if *metricsPath != "/" {
		landingConfig := web.LandingConfig{
			Name:        "Jellyfin Exporter",
			Description: "Prometheus Jellyfin Exporter",
			Version:     version.Info(),
			Links: []web.LandingLinks{
				{
					Address: *metricsPath,
					Text:    "Metrics",
				},
			},
		}
		landingPage, err := web.NewLandingPage(landingConfig)
		if err != nil {
			logger.Error(err.Error())
			os.Exit(1)
		}
		http.Handle("/", landingPage)
	}

	server := &http.Server{}
	if err := web.ListenAndServe(server, toolkitFlags, logger); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}
