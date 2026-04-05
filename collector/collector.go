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

const namespace = "jellyfin"

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

const (
	defaultEnabled  = true
	defaultDisabled = false
)

var (
	factories              = make(map[string]func(logger *slog.Logger) (Collector, error))
	initiatedCollectorsMtx = sync.Mutex{}
	initiatedCollectors    = make(map[string]Collector)
	collectorState         = make(map[string]*bool)
	forcedCollectors       = map[string]bool{}
)

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

type JellyfinCollector struct {
	Collectors map[string]Collector
	logger     *slog.Logger
}

func DisableDefaultCollectors() {
	for c := range collectorState {
		if _, ok := forcedCollectors[c]; !ok {
			*collectorState[c] = false
		}
	}
}

func collectorFlagAction(collector string) func(ctx *kingpin.ParseContext) error {
	return func(ctx *kingpin.ParseContext) error {
		forcedCollectors[collector] = true
		return nil
	}
}

func NewJellyfinCollector(logger *slog.Logger, filters ...string) (*JellyfinCollector, error) {
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

func (n JellyfinCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- scrapeDurationDesc
	ch <- scrapeSuccessDesc
}

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

type Collector interface {
	Update(ch chan<- prometheus.Metric) error
}

var ErrNoData = errors.New("collector returned no data")

func IsNoDataError(err error) bool {
	return errors.Is(err, ErrNoData)
}
