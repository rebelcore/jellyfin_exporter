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

//go:build !noactivity

package collector

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

var (
	jellyfinReportDays = kingpin.Flag("collector.activity.days", "Jellyfin Playback Reporting search in days (Default to 100 Years).").Default("36525").String()
)

type JellyfinUserActivity struct {
	LatestDate    string  `json:"latest_date"`
	UserID        string  `json:"user_id"`
	TotalCount    float64 `json:"total_count"`
	TotalTime     float64 `json:"total_time"`
	ItemName      string  `json:"item_name"`
	ClientName    string  `json:"client_name"`
	UserName      string  `json:"user_name"`
	HasImage      bool    `json:"has_image"`
	LastSeen      string  `json:"last_seen"`
	TotalPlayTime string  `json:"total_play_time"`
}

type activityCollector struct {
	activityReport *prometheus.Desc
	logger         *slog.Logger
}

func init() {
	registerCollector("activity", defaultDisabled, NewActivityCollector)
}

func NewActivityCollector(logger *slog.Logger) (Collector, error) {
	const subsystem = "activity"
	activityReport := prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "count"),
		"Playback Reporting activity.",
		[]string{
			"user_id",
			"username",
			"last_seen",
			"total_play_time",
		}, nil,
	)
	return &activityCollector{
		activityReport: activityReport,
		logger:         logger,
	}, nil
}

func getUserActivity(jellyfinURL, jellyfinToken, days string) ([]JellyfinUserActivity, error) {
	jellyfinAPIURL := fmt.Sprintf("%s/user_usage_stats/user_activity?days=%s", jellyfinURL, days)
	rawData := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)
	rawBody, err := utils.CoerceToJSONBytes(rawData)
	if err != nil {
		return nil, err
	}
	var activityList []JellyfinUserActivity
	if err := json.Unmarshal(rawBody, &activityList); err != nil {
		return nil, fmt.Errorf("unexpected response from Jellyfin API: %w", err)
	}
	return activityList, nil
}

func (c *activityCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, err := config.JellyfinInfo(c.logger)
	if err != nil {
		c.logger.Error("Failed to get Jellyfin config", "error", err)
		return err
	}
	activityList, err := getUserActivity(jellyfinURL, jellyfinToken, *jellyfinReportDays)
	if err != nil {
		c.logger.Error("Failed to get user activity", "error", err)
		return err
	}
	for _, activity := range activityList {
		c.logger.Debug("Jellyfin Playback Reporting for", "User", activity.UserName, "Title", activity.ItemName)
		ch <- prometheus.MustNewConstMetric(
			c.activityReport,
			prometheus.CounterValue,
			activity.TotalCount,
			activity.UserID,
			activity.UserName,
			strings.TrimSpace(activity.LastSeen),
			strings.TrimSpace(activity.TotalPlayTime),
		)
	}
	return nil
}
