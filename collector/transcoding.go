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

//go:build !notranscoding

package collector

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

// transcodingCollector reports active transcoding sessions: a global active
// flag and session count, plus per-session detail (codecs, bitrate, framerate,
// resolution, direct-stream flags and transcode reasons) keyed by session_id.
type transcodingCollector struct {
	active             *prometheus.Desc
	sessions           *prometheus.Desc
	sessionInfo        *prometheus.Desc
	sessionPaused      *prometheus.Desc
	sessionBitrateBps  *prometheus.Desc
	sessionFramerate   *prometheus.Desc
	sessionCompletion  *prometheus.Desc
	sessionWidth       *prometheus.Desc
	sessionHeight      *prometheus.Desc
	sessionVideoDirect *prometheus.Desc
	sessionAudioDirect *prometheus.Desc
	sessionReason      *prometheus.Desc
	logger             *slog.Logger
}

func init() {
	registerCollector("transcoding", defaultDisabled, NewTranscodingCollector)
}

// NewTranscodingCollector builds the transcoding collector and its descriptors.
func NewTranscodingCollector(logger *slog.Logger) (Collector, error) {
	const subsystem = "transcoding"
	return &transcodingCollector{
		active: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "active"),
			"Whether there are any active transcoding sessions (1 = yes, 0 = no).",
			nil,
			nil,
		),
		sessions: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "sessions"),
			"Number of active transcoding sessions.",
			nil,
			nil,
		),
		sessionInfo: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "session_info"),
			"Information about active transcoding sessions.",
			[]string{
				"session_id",
				"user_id",
				"username",
				"device",
				"client",
				"client_version",
				"ip_address",
				"media_type",
				"title",
				"series_title",
				"series_season",
				"series_episode",
				"container",
				"video_codec",
				"audio_codec",
				"audio_channels",
				"hardware_acceleration",
				"method",
			},
			nil,
		),
		sessionPaused: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "session_paused"),
			"Whether the transcoding session is paused (1 = yes, 0 = no).",
			[]string{"session_id"},
			nil,
		),
		sessionBitrateBps: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "session_bitrate_bits_per_second"),
			"Reported transcoding bitrate for the session, in bits per second.",
			[]string{"session_id"},
			nil,
		),
		sessionFramerate: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "session_framerate"),
			"Reported transcoding framerate for the session.",
			[]string{"session_id"},
			nil,
		),
		sessionCompletion: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "session_completion_percentage"),
			"Reported transcoding completion percentage for the session.",
			[]string{"session_id"},
			nil,
		),
		sessionWidth: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "session_width_pixels"),
			"Reported transcoding output width for the session, in pixels.",
			[]string{"session_id"},
			nil,
		),
		sessionHeight: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "session_height_pixels"),
			"Reported transcoding output height for the session, in pixels.",
			[]string{"session_id"},
			nil,
		),
		sessionVideoDirect: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "session_video_direct"),
			"Whether the transcoding session is direct video (1 = yes, 0 = no).",
			[]string{"session_id"},
			nil,
		),
		sessionAudioDirect: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "session_audio_direct"),
			"Whether the transcoding session is direct audio (1 = yes, 0 = no).",
			[]string{"session_id"},
			nil,
		),
		sessionReason: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "session_reason"),
			"Transcode reasons for the session (1 = reason applies).",
			[]string{"session_id", "reason"},
			nil,
		),
		logger: logger,
	}, nil
}

// isSessionTranscoding reports whether a session counts as transcoding: either
// its play method is "Transcode" or the server attached TranscodingInfo.
func isSessionTranscoding(session utils.JellyfinSession) bool {
	if session.PlayState != nil && strings.EqualFold(session.PlayState.PlayMethod, "Transcode") {
		return true
	}
	return session.TranscodingInfo != nil
}

// transcodingLabels builds the label set for jellyfin_transcoding_session_info,
// using "unknown" for a missing session id and tolerating an absent PlayState,
// NowPlayingItem or TranscodingInfo.
func transcodingLabels(session utils.JellyfinSession) []string {
	sessionID := session.Id
	if sessionID == "" {
		sessionID = "unknown"
	}

	state := session.PlayState
	method := ""
	if state != nil {
		method = strings.ToLower(state.PlayMethod)
	}

	item := session.NowPlayingItem
	mediaType, title, seriesTitle := "", "", ""
	if item != nil {
		mediaType = item.Type
		title = item.Name
		seriesTitle = item.SeriesName
	}

	ti := session.TranscodingInfo
	container, videoCodec, audioCodec, audioChannels, hwAccel := "", "", "", "", ""
	if ti != nil {
		container = ti.Container
		videoCodec = ti.VideoCodec
		audioCodec = ti.AudioCodec
		if ti.AudioChannels > 0 {
			audioChannels = strconv.Itoa(ti.AudioChannels)
		}
		hwAccel = ti.HardwareAccelerationType
	}

	return []string{
		sessionID,
		session.UserId,
		session.UserName,
		session.DeviceName,
		session.Client,
		session.ApplicationVersion,
		session.RemoteEndPoint,
		mediaType,
		title,
		seriesTitle,
		item.Season(),
		item.Episode(),
		container,
		videoCodec,
		audioCodec,
		audioChannels,
		hwAccel,
		method,
	}
}

// Update counts the active transcoding sessions, emits per-session detail for
// each, and reports the jellyfin_transcoding_active and
// jellyfin_transcoding_sessions summary metrics.
func (c *transcodingCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, err := config.JellyfinInfo(c.logger)
	if err != nil {
		c.logger.Error("Failed to get Jellyfin config", "error", err)
		return err
	}

	sessions, err := utils.GetActiveSessions(jellyfinURL, jellyfinToken)
	if err != nil {
		c.logger.Error("Failed to get sessions", "error", err)
		return err
	}

	// Walk the active sessions, emitting per-session detail for each one that is
	// transcoding and counting them for the summary metrics below.
	count := 0
	for _, session := range sessions {
		if !isSessionTranscoding(session) {
			continue
		}
		count++

		labels := transcodingLabels(session)
		ch <- prometheus.MustNewConstMetric(c.sessionInfo, prometheus.GaugeValue, 1, labels...)

		sessionID := labels[0]
		paused := 0.0
		if session.PlayState != nil && session.PlayState.IsPaused {
			paused = 1.0
		}
		ch <- prometheus.MustNewConstMetric(c.sessionPaused, prometheus.GaugeValue, paused, sessionID)

		if session.TranscodingInfo != nil {
			ti := session.TranscodingInfo
			ch <- prometheus.MustNewConstMetric(c.sessionBitrateBps, prometheus.GaugeValue, float64(ti.Bitrate), sessionID)
			ch <- prometheus.MustNewConstMetric(c.sessionFramerate, prometheus.GaugeValue, ti.Framerate, sessionID)
			ch <- prometheus.MustNewConstMetric(c.sessionCompletion, prometheus.GaugeValue, ti.CompletionPercentage, sessionID)
			ch <- prometheus.MustNewConstMetric(c.sessionWidth, prometheus.GaugeValue, float64(ti.Width), sessionID)
			ch <- prometheus.MustNewConstMetric(c.sessionHeight, prometheus.GaugeValue, float64(ti.Height), sessionID)
			ch <- prometheus.MustNewConstMetric(c.sessionVideoDirect, prometheus.GaugeValue, utils.BoolToFloat(ti.IsVideoDirect), sessionID)
			ch <- prometheus.MustNewConstMetric(c.sessionAudioDirect, prometheus.GaugeValue, utils.BoolToFloat(ti.IsAudioDirect), sessionID)

			// One series per non-empty transcode reason, so a specific reason
			// (e.g. "VideoCodecNotSupported") can be matched as a label.
			for _, reason := range ti.TranscodeReasons {
				reason = strings.TrimSpace(reason)
				if reason == "" {
					continue
				}
				ch <- prometheus.MustNewConstMetric(c.sessionReason, prometheus.GaugeValue, 1, sessionID, reason)
			}
		}
	}

	// Summary metrics: a single active flag plus the total transcoding count.
	active := 0.0
	if count > 0 {
		active = 1.0
	}
	c.logger.Debug("Jellyfin transcoding state", "active", active, "sessions", count)
	ch <- prometheus.MustNewConstMetric(c.active, prometheus.GaugeValue, active)
	ch <- prometheus.MustNewConstMetric(c.sessions, prometheus.GaugeValue, float64(count))

	return nil
}
