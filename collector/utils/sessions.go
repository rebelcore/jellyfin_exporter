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
)

type PlayState struct {
	PositionTicks *int64 `json:"PositionTicks"`
	IsPaused      bool   `json:"IsPaused"`
	PlayMethod    string `json:"PlayMethod"`
}

type NowPlayingItem struct {
	Name         string `json:"Name"`
	Type         string `json:"Type"`
	SeriesName   string `json:"SeriesName,omitempty"`
	ParentIndex  int    `json:"ParentIndexNumber,omitempty"`
	IndexNumber  int    `json:"IndexNumber,omitempty"`
	RunTimeTicks *int64 `json:"RunTimeTicks,omitempty"`
}

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

func GetActiveSessions(jellyfinURL, jellyfinToken string) ([]JellyfinSession, error) {
	values := url.Values{}
	values.Set("activeWithinSeconds", "60")
	return getSessions(jellyfinURL, jellyfinToken, values)
}

func GetNowPlayingSessions(jellyfinURL, jellyfinToken string) ([]JellyfinSession, error) {
	values := url.Values{}
	values.Set("activeWithinSeconds", "60")
	values.Set("IsPlaying", "true")
	return getSessions(jellyfinURL, jellyfinToken, values)
}

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
