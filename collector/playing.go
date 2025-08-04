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
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

type PlayState struct {
	PositionTicks       int64  `json:"PositionTicks"`
	CanSeek             bool   `json:"CanSeek"`
	IsPaused            bool   `json:"IsPaused"`
	IsMuted             bool   `json:"IsMuted"`
	AudioStreamIndex    int    `json:"AudioStreamIndex"`
	SubtitleStreamIndex int    `json:"SubtitleStreamIndex"`
	MediaSourceId       string `json:"MediaSourceId"`
	PlayMethod          string `json:"PlayMethod"`
	RepeatMode          string `json:"RepeatMode"`
	PlaybackOrder       string `json:"PlaybackOrder"`
}

type NowPlayingItem struct {
	Name        string `json:"Name"`
	Type        string `json:"Type"`
	SeriesName  string `json:"SeriesName,omitempty"`
	ParentIndex int    `json:"ParentIndexNumber,omitempty"`
	IndexNumber int    `json:"IndexNumber,omitempty"`
}

type JellyfinSession struct {
	PlayState          *PlayState      `json:"PlayState"`
	UserId             string          `json:"UserId"`
	UserName           string          `json:"UserName"`
	DeviceName         string          `json:"DeviceName"`
	Client             string          `json:"Client"`
	ApplicationVersion string          `json:"ApplicationVersion"`
	RemoteEndPoint     string          `json:"RemoteEndPoint"`
	LastActivityDate   string          `json:"LastActivityDate"`
	NowPlayingItem     *NowPlayingItem `json:"NowPlayingItem"`
}

type playingCollector struct {
	nowPlaying *prometheus.Desc
	logger     *slog.Logger
}

func init() {
	registerCollector("playing", defaultEnabled, NewPlayingCollector)
}

func NewPlayingCollector(logger *slog.Logger) (Collector, error) {
	const subsystem = "now_playing"
	nowPlaying := prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "state"),
		"Jellyfin currently playing sessions.",
		[]string{
			"user_id", "username", "device", "type", "title", "series_title", "series_season", "series_episode", "method",
		}, nil,
	)
	return &playingCollector{
		nowPlaying: nowPlaying,
		logger:     logger,
	}, nil
}

func getNowPlayingSessions(jellyfinURL, jellyfinToken string) ([]JellyfinSession, error) {
	jellyfinAPIURL := fmt.Sprintf("%s/Sessions?IsPlaying=true", jellyfinURL)
	rawData := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)
	rawBody, err := utils.CoerceToJSONBytes(rawData)
	if err != nil {
		return nil, err
	}
	var sessions []JellyfinSession
	if err := json.Unmarshal(rawBody, &sessions); err != nil {
		return nil, fmt.Errorf("unexpected response from Jellyfin API: %w", err)
	}
	return sessions, nil
}

func (c *playingCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, err := config.JellyfinInfo(c.logger)
	if err != nil {
		c.logger.Error("Failed to get Jellyfin config", "error", err)
		return err
	}
	sessions, err := getNowPlayingSessions(jellyfinURL, jellyfinToken)
	if err != nil {
		c.logger.Error("Failed to get sessions", "error", err)
		return err
	}
	for _, session := range sessions {
		state := 1.0
		playMethod := ""
		mediaType := ""
		title := ""
		seriesTitle := ""
		season := ""
		episode := ""

		if session.PlayState != nil {
			if session.PlayState.IsPaused {
				state = 0.0
			}
			playMethod = strings.ToLower(session.PlayState.PlayMethod)
		}
		if session.NowPlayingItem != nil {
			mediaType = session.NowPlayingItem.Type
			title = session.NowPlayingItem.Name
			seriesTitle = session.NowPlayingItem.SeriesName
			if session.NowPlayingItem.ParentIndex > 0 {
				season = fmt.Sprintf("S%d", session.NowPlayingItem.ParentIndex)
			}
			if session.NowPlayingItem.IndexNumber > 0 {
				episode = fmt.Sprintf("E%d", session.NowPlayingItem.IndexNumber)
			}
		}
		c.logger.Debug("Jellyfin Now Playing", "User", session.UserName, "Title", title)
		ch <- prometheus.MustNewConstMetric(
			c.nowPlaying,
			prometheus.GaugeValue,
			state,
			session.UserId,
			session.UserName,
			session.DeviceName,
			mediaType,
			title,
			seriesTitle,
			season,
			episode,
			playMethod,
		)
	}
	return nil
}
