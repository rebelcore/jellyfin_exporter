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

//go:build !nosystem
// +build !nosystem

package collector

import (
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

type systemCollector struct {
	systemUp *prometheus.Desc
	logger   *slog.Logger
}

func init() {
	registerCollector("system", defaultEnabled, NewSystemCollector)
}

func NewSystemCollector(logger *slog.Logger) (Collector, error) {

	const subsystem = "system"
	systemUp := prometheus.NewDesc(
		namespace+"_up",
		"Jellyfin Media System status.",
		[]string{}, nil,
	)
	return &systemCollector{
		systemUp: systemUp,
		logger:   logger,
	}, nil
}

func (c *systemCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, nil := config.JellyfinInfo(c.logger)

	jellyfinAPIURL := fmt.Sprintf("%s/System/Ping", jellyfinURL)
	rawData := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)
	systemUpValue := 0
	if rawData == "Jellyfin Server" {
		systemUpValue = 1
	}
	c.logger.Debug("Jellyfin Media System state", "Up", systemUpValue)
	ch <- prometheus.MustNewConstMetric(c.systemUp, prometheus.CounterValue, float64(systemUpValue))

	return nil
}
