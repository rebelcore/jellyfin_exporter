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

//go:build !noplaying

package collector

import (
	"log/slog"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

type playingCollector struct {
	nowPlaying       *prometheus.Desc
	progress         *prometheus.Desc
	positionSeconds  *prometheus.Desc
	durationSeconds  *prometheus.Desc
	remainingSeconds *prometheus.Desc
	logger           *slog.Logger
}

func init() {
	registerCollector("playing", defaultEnabled, NewPlayingCollector)
}

func NewPlayingCollector(logger *slog.Logger) (Collector, error) {
	const subsystem = "now_playing"

	labelNames := []string{
		"user_id", "username", "device", "type", "title", "series_title", "series_season", "series_episode", "method",
	}

	return &playingCollector{
		nowPlaying: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "state"),
			"Jellyfin currently playing sessions. (1 = playing, 0 = paused)",
			labelNames,
			nil,
		),
		progress: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "progress"),
			"Percent played for the currently playing session, if run time is known.",
			labelNames,
			nil,
		),
		positionSeconds: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "position"),
			"Current playback position in seconds.",
			labelNames,
			nil,
		),
		durationSeconds: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "duration"),
			"Media duration in seconds.",
			labelNames,
			nil,
		),
		remainingSeconds: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "remaining"),
			"Remaining media time in seconds.",
			labelNames,
			nil,
		),
		logger: logger,
	}, nil
}

func (c *playingCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, err := config.JellyfinInfo(c.logger)
	if err != nil {
		c.logger.Error("Failed to get Jellyfin config", "error", err)
		return err
	}
	sessions, err := utils.GetNowPlayingSessions(jellyfinURL, jellyfinToken)
	if err != nil {
		c.logger.Error("Failed to get sessions", "error", err)
		return err
	}
	for _, session := range sessions {
		state, playMethod, mediaType, title, seriesTitle, season, episode := nowPlayingValues(session)
		if title == "" {
			continue
		}
		c.logger.Debug("Jellyfin Now Playing", "User", session.UserName, "Title", title)

		labelValues := []string{
			session.UserId,
			session.UserName,
			session.DeviceName,
			mediaType,
			title,
			seriesTitle,
			season,
			episode,
			playMethod,
		}

		ch <- prometheus.MustNewConstMetric(c.nowPlaying, prometheus.GaugeValue, state, labelValues...)
		if percent, ok := nowPlayingProgressPercent(session); ok {
			ch <- prometheus.MustNewConstMetric(c.progress, prometheus.GaugeValue, percent, labelValues...)
		}

		if mediaType != "Movie" && mediaType != "Episode" {
			continue
		}

		if pos, ok := nowPlayingPositionSeconds(session); ok {
			ch <- prometheus.MustNewConstMetric(c.positionSeconds, prometheus.GaugeValue, pos, labelValues...)
		}
		if dur, ok := nowPlayingDurationSeconds(session); ok {
			ch <- prometheus.MustNewConstMetric(c.durationSeconds, prometheus.GaugeValue, dur, labelValues...)
		}
		if rem, ok := nowPlayingRemainingSeconds(session); ok {
			ch <- prometheus.MustNewConstMetric(c.remainingSeconds, prometheus.GaugeValue, rem, labelValues...)
		}
	}
	return nil
}

func nowPlayingValues(session utils.JellyfinSession) (state float64, playMethod, mediaType, title, seriesTitle, season, episode string) {
	if session.PlayState == nil {
		state = 0.0
	} else if session.PlayState.IsPaused {
		state = 0.0
	} else {
		state = 1.0
	}
	if session.PlayState != nil {
		playMethod = strings.ToLower(session.PlayState.PlayMethod)
	}
	if session.NowPlayingItem != nil {
		mediaType = session.NowPlayingItem.Type
		title = session.NowPlayingItem.Name
		seriesTitle = session.NowPlayingItem.SeriesName
		season = session.NowPlayingItem.Season()
		episode = session.NowPlayingItem.Episode()
	}

	return state, playMethod, mediaType, title, seriesTitle, season, episode
}

func nowPlayingProgressPercent(session utils.JellyfinSession) (float64, bool) {
	if session.PlayState == nil || session.PlayState.PositionTicks == nil || session.NowPlayingItem == nil || session.NowPlayingItem.RunTimeTicks == nil {
		return 0, false
	}
	runTimeTicks := *session.NowPlayingItem.RunTimeTicks
	if runTimeTicks <= 0 {
		return 0, false
	}
	positionTicks := *session.PlayState.PositionTicks
	if positionTicks < 0 {
		positionTicks = 0
	}
	percent := (float64(positionTicks) / float64(runTimeTicks)) * 100.0
	if percent > 100 {
		percent = 100
	}
	return percent, true
}

const ticksPerSecond = 10_000_000

func ticksToSeconds(ticks int64) float64 {
	return float64(ticks) / float64(ticksPerSecond)
}

func nowPlayingPositionSeconds(session utils.JellyfinSession) (float64, bool) {
	if session.PlayState == nil || session.PlayState.PositionTicks == nil {
		return 0, false
	}
	return ticksToSeconds(*session.PlayState.PositionTicks), true
}

func nowPlayingDurationSeconds(session utils.JellyfinSession) (float64, bool) {
	if session.NowPlayingItem == nil || session.NowPlayingItem.RunTimeTicks == nil {
		return 0, false
	}
	if *session.NowPlayingItem.RunTimeTicks <= 0 {
		return 0, false
	}
	return ticksToSeconds(*session.NowPlayingItem.RunTimeTicks), true
}

func nowPlayingRemainingSeconds(session utils.JellyfinSession) (float64, bool) {
	pos, ok := nowPlayingPositionSeconds(session)
	if !ok {
		return 0, false
	}
	dur, ok := nowPlayingDurationSeconds(session)
	if !ok {
		return 0, false
	}
	rem := dur - pos
	if rem < 0 {
		rem = 0
	}
	return rem, true
}
