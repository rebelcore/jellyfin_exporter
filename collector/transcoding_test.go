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
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/rebelcore/jellyfin_exporter/collector/utils"
)

func TestGetNowPlayingSessions_TranscodingInfo(t *testing.T) {
	const token = "abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "/Sessions", r.URL.Path; want != have {
			t.Fatalf("want path %q, have %q", want, have)
		}
		if want, have := "60", r.URL.Query().Get("activeWithinSeconds"); want != have {
			t.Fatalf("want activeWithinSeconds=%q, have %q", want, have)
		}
		if got := r.URL.Query().Get("IsPlaying"); got != "" {
			t.Fatalf("did not expect IsPlaying query param, got %q", got)
		}
		if want, have := "MediaBrowser Token="+token, r.Header.Get("Authorization"); want != have {
			t.Fatalf("want Authorization header %q, have %q", want, have)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"Id":"s1",
				"UserId":"u1",
				"UserName":"Alice",
				"Client":"Web",
				"ApplicationVersion":"1.0",
				"DeviceName":"TV",
				"RemoteEndPoint":"1.2.3.4",
				"PlayState":{"PlayMethod":"Transcode","IsPaused":false},
				"NowPlayingItem":{"Type":"Episode","Name":"Ep","SeriesName":"Show","ParentIndexNumber":2,"IndexNumber":3},
				"TranscodingInfo":{
					"AudioCodec":"aac",
					"VideoCodec":"h264",
					"Container":"ts",
					"IsVideoDirect":false,
					"IsAudioDirect":false,
					"Bitrate":1234567,
					"Framerate":23.976,
					"CompletionPercentage":50,
					"Width":1920,
					"Height":1080,
					"AudioChannels":2,
					"HardwareAccelerationType":"vaapi",
					"TranscodeReasons":["ContainerNotSupported","VideoCodecNotSupported"]
				}
			}
		]`))
	}))
	defer srv.Close()

	sessions, err := utils.GetActiveSessions(srv.URL, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	s := sessions[0]
	if s.Id != "s1" || s.UserId != "u1" || s.UserName != "Alice" {
		t.Fatalf("unexpected session identity: %#v", s)
	}
	if s.TranscodingInfo == nil {
		t.Fatalf("expected TranscodingInfo")
	}
	if want, have := int64(1234567), s.TranscodingInfo.Bitrate; want != have {
		t.Fatalf("want bitrate %d, have %d", want, have)
	}
}

func TestGetActiveSessions_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := utils.GetActiveSessions(srv.URL, "token")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestGetActiveSessions_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer srv.Close()

	_, err := utils.GetActiveSessions(srv.URL, "token")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestIsSessionTranscoding(t *testing.T) {
	if isSessionTranscoding(utils.JellyfinSession{}) {
		t.Fatalf("expected false")
	}
	if !isSessionTranscoding(utils.JellyfinSession{PlayState: &utils.PlayState{PlayMethod: "Transcode"}}) {
		t.Fatalf("expected true when PlayMethod=Transcode")
	}
	if !isSessionTranscoding(utils.JellyfinSession{TranscodingInfo: &utils.TranscodingInfo{Container: "ts"}}) {
		t.Fatalf("expected true when TranscodingInfo present")
	}
}

func TestTranscodingLabels(t *testing.T) {
	session := utils.JellyfinSession{
		Id:                 "s1",
		UserId:             "u1",
		UserName:           "Alice",
		DeviceName:         "TV",
		Client:             "Web",
		ApplicationVersion: "1.0",
		RemoteEndPoint:     "1.2.3.4",
		PlayState:          &utils.PlayState{PlayMethod: "Transcode"},
		NowPlayingItem: &utils.NowPlayingItem{
			Type:        "Episode",
			Name:        "Ep",
			SeriesName:  "Show",
			ParentIndex: 2,
			IndexNumber: 3,
		},
		TranscodingInfo: &utils.TranscodingInfo{
			Container:                "ts",
			VideoCodec:               "h264",
			AudioCodec:               "aac",
			AudioChannels:            2,
			HardwareAccelerationType: "vaapi",
		},
	}

	want := []string{
		"s1",
		"u1",
		"Alice",
		"TV",
		"Web",
		"1.0",
		"1.2.3.4",
		"Episode",
		"Ep",
		"Show",
		"S2",
		"E3",
		"ts",
		"h264",
		"aac",
		"2",
		"vaapi",
		"transcode",
	}
	if got := transcodingLabels(session); !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected labels:\nwant %#v\ngot  %#v", want, got)
	}
}

func TestTranscodingLabels_UnknownSessionID(t *testing.T) {
	session := utils.JellyfinSession{
		UserId:             "u1",
		UserName:           "Alice",
		DeviceName:         "TV",
		Client:             "Web",
		ApplicationVersion: "1.0",
		RemoteEndPoint:     "1.2.3.4",
		PlayState:          &utils.PlayState{PlayMethod: "Transcode"},
	}
	got := transcodingLabels(session)
	if got[0] != "unknown" {
		t.Fatalf("expected unknown session id, got %q", got[0])
	}
}

func TestTranscodingCollectorUpdate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := NewTranscodingCollector(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ch := make(chan prometheus.Metric, 50)
	if err := c.Update(ch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(ch)

	var (
		metrics    int
		activeSeen bool
	)
	for m := range ch {
		metrics++
		if strings.Contains(m.Desc().String(), "jellyfin_transcoding_active") {
			var dm dto.Metric
			if err := m.Write(&dm); err != nil {
				t.Fatalf("write metric: %v", err)
			}
			if dm.Gauge == nil || dm.Gauge.GetValue() != 1.0 {
				t.Fatalf("expected transcoding active=1, got %#v", dm.Gauge)
			}
			activeSeen = true
		}
	}
	if !activeSeen {
		t.Fatalf("expected transcoding active metric")
	}
	if metrics != 13 {
		t.Fatalf("expected 13 metrics, got %d", metrics)
	}
}
