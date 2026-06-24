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

// Package collector implements the Jellyfin exporter's metric collectors and
// the registry that wires them together.
//
// Each metric source lives in its own file and registers itself from an init()
// function via registerCollector, which also defines a --collector.<name> flag
// to enable or disable it. At scrape time NewJellyfinCollector builds the set of
// enabled collectors (optionally narrowed by an explicit filter list), and
// JellyfinCollector.Collect runs them concurrently, recording a per-collector
// duration and success metric. Individual collectors implement the Collector
// interface and may return ErrNoData to signal "nothing to report" without
// being counted as a failure.
package collector

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
)

// namespace prefixes every metric name the exporter emits (e.g. jellyfin_up,
// jellyfin_system_info).
const namespace = "jellyfin"

// scrapeDurationDesc and scrapeSuccessDesc are emitted once per collector on
// every scrape, letting operators see how long each collector took and whether
// it succeeded.
var (
	scrapeDurationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "scrape", "collector_duration_seconds"),
		"jellyfin_exporter: Duration of a collector scrape.",
		[]string{"collector"},
		nil,
	)
	scrapeSuccessDesc = prometheus.NewDesc(
		prometheus.BuildFQName(namespace, "scrape", "collector_success"),
		"jellyfin_exporter: Whether a collector succeeded.",
		[]string{"collector"},
		nil,
	)
)

// defaultEnabled and defaultDisabled make the registerCollector call sites
// self-documenting about whether a collector is on by default.
const (
	defaultEnabled  = true
	defaultDisabled = false
)

// Collector registry state, populated by registerCollector during package init
// and read at scrape time:
//   - factories:           constructor for each collector name.
//   - initiatedCollectors: collector instances, built lazily and reused across
//     scrapes; guarded by initiatedCollectorsMtx.
//   - collectorState:      pointer to each collector's enabled flag value.
//   - forcedCollectors:    collectors explicitly toggled on the command line,
//     which DisableDefaultCollectors must not override.
var (
	factories              = make(map[string]func(logger *slog.Logger) (Collector, error))
	initiatedCollectorsMtx = sync.Mutex{}
	initiatedCollectors    = make(map[string]Collector)
	collectorState         = make(map[string]*bool)
	forcedCollectors       = map[string]bool{}
)

// registerCollector wires a collector into the registry: it defines the
// --collector.<name> flag (defaulting to enabled or disabled per
// isDefaultEnabled) and records the factory used to build the collector on first
// use. Collectors call this from their init() functions.
func registerCollector(collector string, isDefaultEnabled bool, factory func(logger *slog.Logger) (Collector, error)) {
	var helpDefaultState string
	if isDefaultEnabled {
		helpDefaultState = "enabled"
	} else {
		helpDefaultState = "disabled"
	}

	flagName := fmt.Sprintf("collector.%s", collector)
	flagHelp := fmt.Sprintf("Enable the %s collector (default: %s).", collector, helpDefaultState)
	defaultValue := fmt.Sprintf("%v", isDefaultEnabled)

	flag := kingpin.Flag(flagName, flagHelp).Default(defaultValue).Action(collectorFlagAction(collector)).Bool()
	collectorState[collector] = flag

	factories[collector] = factory
}

// JellyfinCollector is the top-level prometheus.Collector. It fans a single
// scrape out to all of its enabled child collectors, keyed by collector name.
type JellyfinCollector struct {
	Collectors map[string]Collector
	logger     *slog.Logger
}

// DisableDefaultCollectors turns every collector off except those explicitly
// forced on the command line. It backs --collector.disable-defaults, letting
// users opt in to a minimal set with --collector.<name>.
func DisableDefaultCollectors() {
	for c := range collectorState {
		if _, ok := forcedCollectors[c]; !ok {
			*collectorState[c] = false
		}
	}
}

// collectorFlagAction returns a kingpin action that marks a collector as
// explicitly requested, so DisableDefaultCollectors leaves it enabled.
func collectorFlagAction(collector string) func(ctx *kingpin.ParseContext) error {
	return func(ctx *kingpin.ParseContext) error {
		forcedCollectors[collector] = true
		return nil
	}
}

// NewJellyfinCollector returns a JellyfinCollector holding the enabled
// collectors. When filters are supplied, only those collectors are included and
// an unknown or disabled name is an error. Collector instances are created once
// and cached in initiatedCollectors for reuse across scrapes.
func NewJellyfinCollector(logger *slog.Logger, filters ...string) (*JellyfinCollector, error) {
	// Validate any explicit filters first: an unknown or disabled collector name
	// is an error rather than being silently ignored. f is the allow-list; an
	// empty f means "include every enabled collector".
	f := make(map[string]bool)
	for _, filter := range filters {
		enabled, exist := collectorState[filter]
		if !exist {
			return nil, fmt.Errorf("missing collector: %s", filter)
		}
		if !*enabled {
			return nil, fmt.Errorf("disabled collector: %s", filter)
		}
		f[filter] = true
	}
	collectors := make(map[string]Collector)
	initiatedCollectorsMtx.Lock()
	defer initiatedCollectorsMtx.Unlock()
	for key, enabled := range collectorState {
		if !*enabled || (len(f) > 0 && !f[key]) {
			continue
		}
		// Build each collector once and cache it, so later scrapes reuse the
		// same instance (and its registered descriptors) instead of rebuilding.
		if collector, ok := initiatedCollectors[key]; ok {
			collectors[key] = collector
		} else {
			collector, err := factories[key](logger.With("collector", key))
			if err != nil {
				return nil, err
			}
			collectors[key] = collector
			initiatedCollectors[key] = collector
		}
	}
	return &JellyfinCollector{Collectors: collectors, logger: logger}, nil
}

// Describe sends the descriptors for the per-scrape duration and success
// metrics. The child collectors emit "unchecked" const metrics, so their own
// descriptors are intentionally not advertised here.
func (n JellyfinCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- scrapeDurationDesc
	ch <- scrapeSuccessDesc
}

// Collect runs every enabled child collector concurrently (one goroutine each)
// and waits for all of them. Each collector writes directly to the shared
// channel; execute wraps the call to record its duration and success.
func (n JellyfinCollector) Collect(ch chan<- prometheus.Metric) {
	wg := sync.WaitGroup{}
	wg.Add(len(n.Collectors))
	for name, c := range n.Collectors {
		go func(name string, c Collector) {
			execute(name, c, ch, n.logger)
			wg.Done()
		}(name, c)
	}
	wg.Wait()
}

// execute runs one collector's Update, times it, and emits the per-collector
// duration and success gauges. Any returned error sets success=0; ErrNoData is
// logged at debug level rather than as a failure.
func execute(name string, c Collector, ch chan<- prometheus.Metric, logger *slog.Logger) {
	begin := time.Now()
	err := c.Update(ch)
	duration := time.Since(begin)
	var success float64

	if err != nil {
		if IsNoDataError(err) {
			logger.Debug("collector returned no data", "name", name, "duration_seconds", duration.Seconds(), "err", err)
		} else {
			logger.Error("collector failed", "name", name, "duration_seconds", duration.Seconds(), "err", err)
		}
		success = 0
	} else {
		logger.Debug("collector succeeded", "name", name, "duration_seconds", duration.Seconds())
		success = 1
	}
	ch <- prometheus.MustNewConstMetric(scrapeDurationDesc, prometheus.GaugeValue, duration.Seconds(), name)
	ch <- prometheus.MustNewConstMetric(scrapeSuccessDesc, prometheus.GaugeValue, success, name)
}

// Collector is implemented by every metric source. Update collects the current
// metrics and sends them on ch, returning an error (or ErrNoData) on failure.
//
// Every collector's Update follows the same shape: read the connection settings
// via config.JellyfinInfo, fetch the relevant Jellyfin API endpoint, decode the
// response, and emit one or more const metrics. Reading one collector is enough
// to understand them all.
type Collector interface {
	Update(ch chan<- prometheus.Metric) error
}

// ErrNoData is returned by a collector's Update when it has nothing to report
// for this scrape but did not actually fail.
var ErrNoData = errors.New("collector returned no data")

// IsNoDataError reports whether err is, or wraps, ErrNoData.
func IsNoDataError(err error) bool {
	return errors.Is(err, ErrNoData)
}
