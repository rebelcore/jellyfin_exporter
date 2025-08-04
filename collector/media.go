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

//go:build !nomedia

package collector

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

type mediaCollector struct {
	mediaItems *prometheus.Desc
	logger     *slog.Logger
}

func init() {
	registerCollector("media", defaultEnabled, NewMediaCollector)
}

func NewMediaCollector(logger *slog.Logger) (Collector, error) {
	const subsystem = "media"
	mediaItems := prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "count"),
		"Total media items.",
		[]string{"type"}, nil,
	)
	return &mediaCollector{
		mediaItems: mediaItems,
		logger:     logger,
	}, nil
}

func getMediaCounts(jellyfinURL, jellyfinToken string) (map[string]float64, error) {
	jellyfinAPIURL := fmt.Sprintf("%s/Items/Counts", jellyfinURL)
	rawData := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)
	rawBody, err := utils.CoerceToJSONBytes(rawData)
	if err != nil {
		return nil, err
	}
	var counts map[string]float64
	if err := json.Unmarshal(rawBody, &counts); err != nil {
		return nil, fmt.Errorf("unexpected response from Jellyfin API: %w", err)
	}
	return counts, nil
}

func (c *mediaCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, err := config.JellyfinInfo(c.logger)
	if err != nil {
		c.logger.Error("Failed to get Jellyfin config", "error", err)
		return err
	}
	counts, err := getMediaCounts(jellyfinURL, jellyfinToken)
	if err != nil {
		c.logger.Error("Failed to get media counts", "error", err)
		return err
	}
	for name, count := range counts {
		itemName := strings.ReplaceAll(name, "Count", "")
		c.logger.Debug("Jellyfin Media System Total", itemName, count)
		ch <- prometheus.MustNewConstMetric(
			c.mediaItems,
			prometheus.CounterValue,
			count,
			itemName,
		)
	}
	return nil
}
