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
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

// activityFlags holds the activity collector's options. Days is the look-back
// window for the Playback Reporting query, defaulting to ~100 years so the full
// history is included.
var activityFlags struct {
	Days int `name:"collector.activity.days" default:"36525" help:"Jellyfin Playback Reporting search in days (Default to 100 Years)."`
}

// JellyfinUserActivity is one row from the Playback Reporting plugin's
// user_activity report. Field tags are snake_case to match that plugin's API.
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

// activityCollector reports per-user playback totals from the third-party
// Playback Reporting plugin: the play count (jellyfin_activity_count), the play
// time and the time since the last play. It is disabled by default because it
// requires that plugin to be installed.
type activityCollector struct {
	activityReport *prometheus.Desc
	playTime       *prometheus.Desc
	lastSeen       *prometheus.Desc
	logger         *slog.Logger
}

func init() {
	registerCollector("activity", defaultDisabled, NewActivityCollector)
	registerFlags(&activityFlags)
}

// NewActivityCollector builds the activity collector and its descriptors. The
// series are keyed by user alone: the plugin's last_seen and total_play_time
// text changes by the minute, so as labels it started a new series each time.
func NewActivityCollector(logger *slog.Logger) (Collector, error) {
	const subsystem = "activity"

	labelNames := []string{"user_id", "username"}

	return &activityCollector{
		activityReport: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "count"),
			"Number of plays recorded by the Playback Reporting plugin.",
			labelNames, nil,
		),
		playTime: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "play_time_seconds"),
			"Total play time in seconds recorded by the Playback Reporting plugin.",
			labelNames, nil,
		),
		lastSeen: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "last_seen_seconds"),
			"Seconds since the user's most recent play started, rounded down to the minute.",
			labelNames, nil,
		),
		logger: logger,
	}, nil
}

// getUserActivity queries the Playback Reporting plugin's user_activity endpoint
// over the given look-back window (in days) and decodes the result.
func getUserActivity(jellyfinURL, jellyfinToken string, days int) ([]JellyfinUserActivity, error) {
	values := url.Values{}
	values.Set("days", strconv.Itoa(days))
	jellyfinAPIURL := fmt.Sprintf("%s/user_usage_stats/user_activity?%s", jellyfinURL, values.Encode())
	var activityList []JellyfinUserActivity
	if err := utils.GetJSON(jellyfinAPIURL, jellyfinToken, &activityList); err != nil {
		return nil, err
	}
	return activityList, nil
}

// lastSeenUnits is the length in seconds of each unit in the plugin's last_seen
// text. The plugin counts a year as 365 days.
var lastSeenUnits = map[string]float64{
	"year":   365 * 24 * 60 * 60,
	"week":   7 * 24 * 60 * 60,
	"day":    24 * 60 * 60,
	"hour":   60 * 60,
	"minute": 60,
}

// parseLastSeen converts the plugin's last_seen text, such as "2 days 3 hours
// 5 minutes " or "just now", into seconds. The plugin rounds it down to the
// minute.
//
// The latest_date field looks like the better source but is not: the plugin
// shifts it by the server's UTC offset, so it is only right on a server that
// runs in UTC. last_seen is the plugin's own time since then, and is correct
// in any time zone.
func parseLastSeen(text string) (float64, error) {
	fields := strings.Fields(text)
	if len(fields) == 2 && fields[0] == "just" && fields[1] == "now" {
		return 0, nil
	}
	if len(fields) == 0 || len(fields)%2 != 0 {
		return 0, fmt.Errorf("unexpected last_seen %q", text)
	}
	var seconds float64
	for i := 0; i < len(fields); i += 2 {
		n, err := strconv.Atoi(fields[i])
		unit, ok := lastSeenUnits[strings.TrimSuffix(fields[i+1], "s")]
		if err != nil || n < 0 || !ok {
			return 0, fmt.Errorf("unexpected last_seen %q", text)
		}
		seconds += float64(n) * unit
	}
	return seconds, nil
}

// Update emits the play count, play time and time since the last play for
// each user in the Playback Reporting data. The time since the last play is
// left out for a user whose last_seen text cannot be parsed.
func (c *activityCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, err := config.JellyfinInfo(c.logger)
	if err != nil {
		c.logger.Error("Failed to get Jellyfin config", "error", err)
		return err
	}
	activityList, err := getUserActivity(jellyfinURL, jellyfinToken, activityFlags.Days)
	if err != nil {
		c.logger.Error("Failed to get user activity", "error", err)
		return err
	}
	for _, activity := range activityList {
		c.logger.Debug("Jellyfin Playback Reporting for", "User", activity.UserName, "Title", activity.ItemName)
		ch <- prometheus.MustNewConstMetric(c.activityReport, prometheus.GaugeValue, activity.TotalCount, activity.UserID, activity.UserName)
		ch <- prometheus.MustNewConstMetric(c.playTime, prometheus.GaugeValue, activity.TotalTime, activity.UserID, activity.UserName)

		lastSeen, err := parseLastSeen(activity.LastSeen)
		if err != nil {
			c.logger.Debug("Skipping last seen", "User", activity.UserName, "error", err)
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.lastSeen, prometheus.GaugeValue, lastSeen, activity.UserID, activity.UserName)
	}
	return nil
}
