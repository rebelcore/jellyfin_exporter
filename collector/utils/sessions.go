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

package utils

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// PlayState describes the playback state of a session. PositionTicks is a
// pointer so "not reported" is distinguishable from a genuine 0; ticks are
// 100-nanosecond units (see ticksToSeconds in the playing collector).
type PlayState struct {
	PositionTicks *int64 `json:"PositionTicks"`
	IsPaused      bool   `json:"IsPaused"`
	PlayMethod    string `json:"PlayMethod"`
}

// NowPlayingItem is the media item a session is currently playing. The
// series-only fields are empty/zero for movies, and RunTimeTicks is a pointer
// because not every item type reports a runtime.
type NowPlayingItem struct {
	Name         string `json:"Name"`
	Type         string `json:"Type"`
	SeriesName   string `json:"SeriesName,omitempty"`
	ParentIndex  int    `json:"ParentIndexNumber,omitempty"`
	IndexNumber  int    `json:"IndexNumber,omitempty"`
	RunTimeTicks *int64 `json:"RunTimeTicks,omitempty"`
}

// Season returns the season label (e.g. "S2") for an episode, or "" when the
// item has no season. Safe to call on a nil receiver.
func (i *NowPlayingItem) Season() string {
	if i == nil || i.ParentIndex <= 0 {
		return ""
	}
	return "S" + strconv.Itoa(i.ParentIndex)
}

// Episode returns the episode label (e.g. "E3"), or "" when not applicable.
// Safe to call on a nil receiver.
func (i *NowPlayingItem) Episode() string {
	if i == nil || i.IndexNumber <= 0 {
		return ""
	}
	return "E" + strconv.Itoa(i.IndexNumber)
}

// TranscodingInfo is present on a session only while the server is actively
// transcoding it. IsVideoDirect/IsAudioDirect indicate which streams are passed
// through untouched, and TranscodeReasons lists why a transcode was required.
type TranscodingInfo struct {
	AudioCodec               string   `json:"AudioCodec"`
	VideoCodec               string   `json:"VideoCodec"`
	Container                string   `json:"Container"`
	IsVideoDirect            bool     `json:"IsVideoDirect"`
	IsAudioDirect            bool     `json:"IsAudioDirect"`
	Bitrate                  int64    `json:"Bitrate"`
	Framerate                float64  `json:"Framerate"`
	CompletionPercentage     float64  `json:"CompletionPercentage"`
	Width                    int      `json:"Width"`
	Height                   int      `json:"Height"`
	AudioChannels            int      `json:"AudioChannels"`
	HardwareAccelerationType string   `json:"HardwareAccelerationType"`
	TranscodeReasons         []string `json:"TranscodeReasons"`
}

// JellyfinSession is one entry from the /Sessions endpoint. PlayState,
// NowPlayingItem and TranscodingInfo are pointers because they are absent for
// idle sessions, so collectors must nil-check them before dereferencing.
type JellyfinSession struct {
	Id                 string           `json:"Id"`
	PlayState          *PlayState       `json:"PlayState"`
	UserId             string           `json:"UserId"`
	UserName           string           `json:"UserName"`
	DeviceName         string           `json:"DeviceName"`
	Client             string           `json:"Client"`
	ApplicationVersion string           `json:"ApplicationVersion"`
	RemoteEndPoint     string           `json:"RemoteEndPoint"`
	NowPlayingItem     *NowPlayingItem  `json:"NowPlayingItem"`
	TranscodingInfo    *TranscodingInfo `json:"TranscodingInfo"`
}

// GetActiveSessions returns every session seen active within the last 60
// seconds, including idle ones (those without a NowPlayingItem).
func GetActiveSessions(jellyfinURL, jellyfinToken string) ([]JellyfinSession, error) {
	values := url.Values{}
	values.Set("activeWithinSeconds", "60")
	return getSessions(jellyfinURL, jellyfinToken, values)
}

// GetNowPlayingSessions returns only the sessions that are currently playing
// something, using the server-side IsPlaying filter.
func GetNowPlayingSessions(jellyfinURL, jellyfinToken string) ([]JellyfinSession, error) {
	values := url.Values{}
	values.Set("activeWithinSeconds", "60")
	values.Set("IsPlaying", "true")
	return getSessions(jellyfinURL, jellyfinToken, values)
}

// getSessions queries /Sessions with the given filter and decodes the JSON
// array into JellyfinSession values.
func getSessions(jellyfinURL, jellyfinToken string, query url.Values) ([]JellyfinSession, error) {
	jellyfinAPIURL := fmt.Sprintf("%s/Sessions?%s", jellyfinURL, query.Encode())
	rawBody, err := GetHTTP(jellyfinAPIURL, jellyfinToken)
	if err != nil {
		return nil, err
	}
	var sessions []JellyfinSession
	if err := json.Unmarshal(rawBody, &sessions); err != nil {
		return nil, fmt.Errorf("unexpected response from Jellyfin API: %w", err)
	}
	return sessions, nil
}
