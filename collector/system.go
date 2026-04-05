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

package collector

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

type systemCollector struct {
	systemUp          *prometheus.Desc
	info              *prometheus.Desc
	hasPendingRestart *prometheus.Desc
	logger            *slog.Logger
}

type jellyfinSystemInfo struct {
	ServerName             string `json:"ServerName"`
	Version                string `json:"Version"`
	ProductName            string `json:"ProductName"`
	PackageName            string `json:"PackageName"`
	ID                     string `json:"Id"`
	HasPendingRestart      bool   `json:"HasPendingRestart"`
	IsShuttingDown         bool   `json:"IsShuttingDown"`
	SupportsLibraryMonitor bool   `json:"SupportsLibraryMonitor"`
	WebSocketPortNumber    int64  `json:"WebSocketPortNumber"`
	StartupWizardCompleted *bool  `json:"StartupWizardCompleted"`
}

func init() {
	registerCollector("system", defaultEnabled, NewSystemCollector)
}

func NewSystemCollector(logger *slog.Logger) (Collector, error) {
	const subsystem = "system"

	return &systemCollector{
		systemUp: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "up"),
			"Jellyfin Media System status.",
			nil, nil,
		),
		info: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "info"),
			"Static Jellyfin server information.",
			[]string{"server_id", "server_name", "version", "product_name", "package_name"},
			nil,
		),
		hasPendingRestart: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "pending_restart"),
			"Whether the Jellyfin server has a pending restart (1 = yes, 0 = no).",
			nil,
			nil,
		),
		logger: logger,
	}, nil
}

func getSystemPing(jellyfinURL, jellyfinToken string) (float64, error) {
	jellyfinAPIURL := fmt.Sprintf("%s/System/Ping", jellyfinURL)
	rawBody, err := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)
	if err != nil {
		return 0, err
	}
	return float64(utils.SystemUpValueFromPing(rawBody)), nil
}

func getSystemInfo(jellyfinURL, jellyfinToken string) (*jellyfinSystemInfo, error) {
	jellyfinAPIURL := fmt.Sprintf("%s/System/Info", jellyfinURL)
	rawBody, err := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)
	if err != nil {
		return nil, err
	}
	var info jellyfinSystemInfo
	if err := json.Unmarshal(rawBody, &info); err != nil {
		return nil, fmt.Errorf("unexpected response from Jellyfin API: %w", err)
	}
	return &info, nil
}

func (c *systemCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, err := config.JellyfinInfo(c.logger)
	if err != nil {
		c.logger.Error("Failed to get Jellyfin config", "error", err)
		return err
	}

	ping, err := getSystemPing(jellyfinURL, jellyfinToken)
	if err != nil {
		c.logger.Error("Failed to get system ping", "error", err)
		return err
	}

	info, err := getSystemInfo(jellyfinURL, jellyfinToken)
	if err != nil {
		c.logger.Error("Failed to get system info", "error", err)
		return err
	}

	ch <- prometheus.MustNewConstMetric(c.systemUp, prometheus.GaugeValue, ping)
	ch <- prometheus.MustNewConstMetric(
		c.info,
		prometheus.GaugeValue,
		1,
		info.ID,
		info.ServerName,
		info.Version,
		info.ProductName,
		info.PackageName,
	)
	ch <- prometheus.MustNewConstMetric(c.hasPendingRestart, prometheus.GaugeValue, utils.BoolToFloat(info.HasPendingRestart))

	return nil
}
