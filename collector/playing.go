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
// +build !noplaying

package collector

import (
	"fmt"
	"log/slog"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

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
		namespace+"_"+subsystem,
		"Jellyfin current active users.",
		[]string{"user_id", "username", "type", "title", "series_title", "series_season", "series_episode"}, nil,
	)
	return &playingCollector{
		nowPlaying: nowPlaying,
		logger:     logger,
	}, nil
}

func (c *playingCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, nil := config.JellyfinInfo(c.logger)

	jellyfinAPIURL := fmt.Sprintf("%s/Sessions?IsPlaying=true", jellyfinURL)
	rawData := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)
	data := rawData.([]interface{})

	for _, item := range data {
		sessionMap := item.(map[string]interface{})
		playStateMap := sessionMap["PlayState"].(map[string]interface{})

		playMethod := ""
		playingType := ""
		playingTitle := ""
		playingSeriesTitle := ""
		playingSeriesSeason := ""
		playingSeriesEpisode := ""

		if playStateMap["PlayMethod"] != nil {
			playMethod = playStateMap["PlayMethod"].(string)
		}
		if sessionMap["NowPlayingItem"] != nil {
			nowPlayingMap := sessionMap["NowPlayingItem"].(map[string]interface{})
			playingType = nowPlayingMap["Type"].(string)
			playingTitle = nowPlayingMap["Name"].(string)
			if nowPlayingMap["SeriesName"] != nil {
				playingSeriesTitle = nowPlayingMap["SeriesName"].(string)
				playingSeriesSeason = "S" + strconv.Itoa(int(nowPlayingMap["ParentIndexNumber"].(float64)))
				playingSeriesEpisode = "E" + strconv.Itoa(int(nowPlayingMap["IndexNumber"].(float64)))
			}
		}
		if playMethod != "" {
			c.logger.Debug("Jellyfin Now Playing", "Value", playingTitle+" - "+sessionMap["UserName"].(string))
			ch <- prometheus.MustNewConstMetric(
				c.nowPlaying,
				prometheus.GaugeValue,
				1,
				sessionMap["UserId"].(string),
				sessionMap["UserName"].(string),
				playingType,
				playingTitle,
				playingSeriesTitle,
				playingSeriesSeason,
				playingSeriesEpisode,
			)
		}
	}

	return nil
}
