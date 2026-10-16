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
// function via registerCollector, which also gives it a --[no-]collector.<name>
// flag to enable or disable it (see Flags). At scrape time NewJellyfinCollector
// builds the set of enabled collectors (optionally narrowed by an explicit
// filter list), and JellyfinCollector.Collect runs them concurrently, recording
// a per-collector duration and success metric. Individual collectors implement
// the Collector interface and may return ErrNoData to signal "nothing to
// report", which is logged at debug level rather than as a failure — though, as
// for any returned error, the collector's success metric is still 0.
package collector

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/kong"
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
//   - collectorDefaults:   whether each collector is enabled by default.
//   - forcedCollectors:    collectors explicitly toggled on the command line,
//     which DisableDefaultCollectors must not override.
//   - flagGroups:          collector-specific options added by registerFlags.
var (
	factories              = make(map[string]func(logger *slog.Logger) (Collector, error))
	initiatedCollectorsMtx = sync.Mutex{}
	initiatedCollectors    = make(map[string]Collector)
	collectorState         = make(map[string]*bool)
	collectorDefaults      = make(map[string]bool)
	forcedCollectors       = map[string]bool{}
	flagGroups             []any
)

// registerCollector wires a collector into the registry: it records whether the
// collector is enabled by default, which seeds its --collector.<name> flag (see
// Flags), and the factory used to build the collector on first use. Collectors
// call this from their init() functions.
func registerCollector(collector string, isDefaultEnabled bool, factory func(logger *slog.Logger) (Collector, error)) {
	enabled := isDefaultEnabled
	collectorState[collector] = &enabled
	collectorDefaults[collector] = isDefaultEnabled
	factories[collector] = factory
}

// registerFlags adds a collector's own options to the command line. group must
// be a pointer to a struct whose fields carry kong flag tags. Collectors call
// this from their init() functions, alongside registerCollector.
func registerFlags(group any) {
	flagGroups = append(flagGroups, group)
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

// SnapshotCollectorStates captures which collectors are currently enabled and
// which were explicitly forced, returning a function that restores both.
//
// Collector state is process-wide, so a caller that flips it — most notably
// DisableDefaultCollectors — changes it for everything that follows. Tests use
// this to undo that effect and stay independent of the order they run in.
func SnapshotCollectorStates() func() {
	savedState := make(map[string]bool, len(collectorState))
	for name, enabled := range collectorState {
		savedState[name] = *enabled
	}
	savedForced := make(map[string]bool, len(forcedCollectors))
	for name, forced := range forcedCollectors {
		savedForced[name] = forced
	}

	return func() {
		for name, enabled := range savedState {
			if state, ok := collectorState[name]; ok {
				*state = enabled
			}
		}
		clear(forcedCollectors)
		for name, forced := range savedForced {
			forcedCollectors[name] = forced
		}
	}
}

// toggles is the flag group holding the --[no-]collector.<name> flags, built
// on the first call to Flags.
var (
	togglesOnce sync.Once
	toggles     any
)

// Flags returns the collector flag groups for the exporter's command line: the
// --[no-]collector.<name> toggles plus any collector-specific options. Embed
// each one in the root of the CLI with kong.Embed. Parsing writes the toggles
// straight into the collector state; call MarkForcedCollectors afterwards so
// DisableDefaultCollectors knows which ones were set explicitly.
func Flags() []any {
	togglesOnce.Do(func() { toggles = newToggles() })
	return append([]any{toggles}, flagGroups...)
}

// newToggles builds a struct with one --[no-]collector.<name> bool field per
// registered collector. Kong reads flags from struct tags, but which collectors
// exist is only settled once their init() functions (and the build tags that
// select them) have run, so the struct type is assembled at run time with
// reflect.StructOf. Each collector's state is then pointed at its field, so
// parsing updates collectorState directly.
func newToggles() any {
	names := slices.Sorted(maps.Keys(collectorState))
	fields := make([]reflect.StructField, len(names))
	for i, name := range names {
		helpDefaultState := "disabled"
		if collectorDefaults[name] {
			helpDefaultState = "enabled"
		}
		fields[i] = reflect.StructField{
			Name: fmt.Sprintf("Collector%d", i),
			Type: reflect.TypeFor[bool](),
			Tag: reflect.StructTag(fmt.Sprintf(
				`name:"collector.%s" negatable:"" default:"%t" help:"Enable the %s collector (default: %s)."`,
				name, collectorDefaults[name], name, helpDefaultState,
			)),
		}
	}

	group := reflect.New(reflect.StructOf(fields))
	for i, name := range names {
		field := group.Elem().Field(i).Addr().Interface().(*bool)
		*field = *collectorState[name]
		collectorState[name] = field
	}
	return group.Interface()
}

// MarkForcedCollectors records each collector whose --[no-]collector.<name>
// flag was given on the command line, so DisableDefaultCollectors leaves it as
// set. Call it once after parsing.
func MarkForcedCollectors(ctx *kong.Context) {
	for _, p := range ctx.Path {
		if p.Flag == nil || p.Resolved {
			continue
		}
		if name, ok := strings.CutPrefix(p.Flag.Name, "collector."); ok {
			if _, registered := collectorState[name]; registered {
				forcedCollectors[name] = true
			}
		}
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
// for this scrape. It is logged at debug rather than error level, but like any
// other returned error it still records the collector's scrape as unsuccessful.
var ErrNoData = errors.New("collector returned no data")

// IsNoDataError reports whether err is, or wraps, ErrNoData.
func IsNoDataError(err error) bool {
	return errors.Is(err, ErrNoData)
}
