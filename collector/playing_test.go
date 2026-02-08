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
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
)

func TestGetNowPlayingSessions(t *testing.T) {
	const token = "abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "/Sessions", r.URL.Path; want != have {
			t.Fatalf("want path %q, have %q", want, have)
		}
		if want, have := "60", r.URL.Query().Get("activeWithinSeconds"); want != have {
			t.Fatalf("want activeWithinSeconds=%q, have %q", want, have)
		}
		if want, have := "true", r.URL.Query().Get("IsPlaying"); want != have {
			t.Fatalf("want IsPlaying=%q, have %q", want, have)
		}
		if want, have := "MediaBrowser Token="+token, r.Header.Get("Authorization"); want != have {
			t.Fatalf("want Authorization header %q, have %q", want, have)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"UserId":"u1","UserName":"Alice","DeviceName":"TV","PlayState":{"IsPaused":false,"PlayMethod":"DirectPlay","PositionTicks":250},"NowPlayingItem":{"Type":"Movie","Name":"Title","RunTimeTicks":1000}}
		]`))
	}))
	defer srv.Close()

	sessions, err := utils.GetNowPlayingSessions(srv.URL, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
}

func TestNowPlayingValues(t *testing.T) {
	session := utils.JellyfinSession{
		PlayState: &utils.PlayState{IsPaused: true, PlayMethod: "Transcode"},
		NowPlayingItem: &utils.NowPlayingItem{
			Type:        "Episode",
			Name:        "Ep",
			SeriesName:  "Show",
			ParentIndex: 2,
			IndexNumber: 3,
		},
	}

	state, playMethod, mediaType, title, seriesTitle, season, episode := nowPlayingValues(session)
	if state != 0.0 {
		t.Fatalf("want state=0.0, got %v", state)
	}
	if playMethod != "transcode" {
		t.Fatalf("want playMethod=%q, got %q", "transcode", playMethod)
	}
	if mediaType != "Episode" || title != "Ep" || seriesTitle != "Show" {
		t.Fatalf("unexpected labels: mediaType=%q title=%q seriesTitle=%q", mediaType, title, seriesTitle)
	}
	if season != "S2" || episode != "E3" {
		t.Fatalf("unexpected season/episode: %q/%q", season, episode)
	}

	state, playMethod, mediaType, title, seriesTitle, season, episode = nowPlayingValues(utils.JellyfinSession{})
	if state != 0.0 || playMethod != "" || mediaType != "" || title != "" || seriesTitle != "" || season != "" || episode != "" {
		t.Fatalf("unexpected default values: state=%v playMethod=%q mediaType=%q title=%q seriesTitle=%q season=%q episode=%q",
			state, playMethod, mediaType, title, seriesTitle, season, episode)
	}
}

func TestNowPlayingProgressPercent(t *testing.T) {
	rt := int64(1000)
	pos := int64(250)
	session := utils.JellyfinSession{
		PlayState: &utils.PlayState{PositionTicks: &pos},
		NowPlayingItem: &utils.NowPlayingItem{
			RunTimeTicks: &rt,
		},
	}
	got, ok := nowPlayingProgressPercent(session)
	if !ok {
		t.Fatalf("expected ok")
	}
	if got != 25.0 {
		t.Fatalf("want 25.0, got %v", got)
	}

	zero := int64(0)
	session.NowPlayingItem.RunTimeTicks = &zero
	got, ok = nowPlayingProgressPercent(session)
	if ok {
		t.Fatalf("expected not ok, got %v", got)
	}
	session.NowPlayingItem.RunTimeTicks = &rt

	got, ok = nowPlayingProgressPercent(utils.JellyfinSession{})
	if ok {
		t.Fatalf("expected not ok, got %v", got)
	}

	neg := int64(-123)
	session.PlayState.PositionTicks = &neg
	got, ok = nowPlayingProgressPercent(session)
	if !ok {
		t.Fatalf("expected ok")
	}
	if got != 0.0 {
		t.Fatalf("want 0.0, got %v", got)
	}

	tooFar := int64(2000)
	session.PlayState.PositionTicks = &tooFar
	got, ok = nowPlayingProgressPercent(session)
	if !ok {
		t.Fatalf("expected ok")
	}
	if got != 100.0 {
		t.Fatalf("want 100.0, got %v", got)
	}
}

func TestNowPlayingSeconds(t *testing.T) {
	rt := int64(100000000)  // 10s
	pos := int64(50000000)  // 5s
	neg := int64(-10000000) // -1s

	session := utils.JellyfinSession{
		PlayState:      &utils.PlayState{PositionTicks: &pos},
		NowPlayingItem: &utils.NowPlayingItem{RunTimeTicks: &rt},
	}
	if got, ok := nowPlayingPositionSeconds(session); !ok || got != 5.0 {
		t.Fatalf("want ok=true and 5.0, got ok=%v seconds=%v", ok, got)
	}
	if got, ok := nowPlayingDurationSeconds(session); !ok || got != 10.0 {
		t.Fatalf("want ok=true and 10.0, got ok=%v seconds=%v", ok, got)
	}
	if got, ok := nowPlayingRemainingSeconds(session); !ok || got != 5.0 {
		t.Fatalf("want ok=true and 5.0, got ok=%v seconds=%v", ok, got)
	}

	session.PlayState.PositionTicks = &neg
	if got, ok := nowPlayingPositionSeconds(session); !ok || got != -1.0 {
		t.Fatalf("want ok=true and -1.0, got ok=%v seconds=%v", ok, got)
	}

	zero := int64(0)
	session.NowPlayingItem.RunTimeTicks = &zero
	if _, ok := nowPlayingDurationSeconds(session); ok {
		t.Fatalf("expected ok=false for non-positive duration")
	}

	posBeyond := int64(200000000) // 20s
	session.NowPlayingItem.RunTimeTicks = &rt
	session.PlayState.PositionTicks = &posBeyond
	if got, ok := nowPlayingRemainingSeconds(session); !ok || got != 0 {
		t.Fatalf("want ok=true and remaining=0, got ok=%v remaining=%v", ok, got)
	}

	if _, ok := nowPlayingRemainingSeconds(utils.JellyfinSession{}); ok {
		t.Fatalf("expected ok=false when missing position/duration")
	}

	if _, ok := nowPlayingRemainingSeconds(utils.JellyfinSession{PlayState: &utils.PlayState{}}); ok {
		t.Fatalf("expected ok=false when missing position ticks")
	}

	if _, ok := nowPlayingRemainingSeconds(utils.JellyfinSession{
		PlayState: &utils.PlayState{PositionTicks: &pos},
	}); ok {
		t.Fatalf("expected ok=false when missing now playing item")
	}

	if _, ok := nowPlayingRemainingSeconds(utils.JellyfinSession{
		PlayState:      &utils.PlayState{PositionTicks: &pos},
		NowPlayingItem: &utils.NowPlayingItem{},
	}); ok {
		t.Fatalf("expected ok=false when missing duration ticks")
	}
}

func TestPlayingCollectorUpdate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := NewPlayingCollector(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ch := make(chan prometheus.Metric, 50)
	if err := c.Update(ch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(ch)

	var (
		metrics       int
		positionSeen  bool
		positionValue float64
	)
	for m := range ch {
		metrics++
		if strings.Contains(m.Desc().String(), "jellyfin_now_playing_position") {
			var dm dto.Metric
			if err := m.Write(&dm); err != nil {
				t.Fatalf("write metric: %v", err)
			}
			if dm.Gauge == nil {
				t.Fatalf("expected gauge metric")
			}
			positionValue = dm.Gauge.GetValue()
			positionSeen = true
		}
	}
	if metrics != 6 {
		t.Fatalf("expected 6 metrics, got %d", metrics)
	}
	if !positionSeen {
		t.Fatalf("expected position metric")
	}
	if positionValue != 5.0 {
		t.Fatalf("expected position=5.0 seconds, got %v", positionValue)
	}
}

func TestPlayingCollectorUpdate_SkipsAndUnsupportedTypes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "/Sessions", r.URL.Path; want != have {
			t.Fatalf("want path %q, have %q", want, have)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"UserId":"u1","UserName":"Alice","DeviceName":"TV","PlayState":{"IsPaused":false,"PlayMethod":"DirectPlay","PositionTicks":250},"NowPlayingItem":{"Type":"Movie","Name":"","RunTimeTicks":1000}},
			{"UserId":"u2","UserName":"Bob","DeviceName":"Phone","PlayState":{"IsPaused":false,"PlayMethod":"DirectPlay","PositionTicks":250},"NowPlayingItem":{"Type":"Audio","Name":"Song","RunTimeTicks":1000}}
		]`))
	}))
	defer srv.Close()

	setJellyfinFlags(t, srv.URL, testJellyfinToken)
	t.Cleanup(func() { setJellyfinFlags(t, testJellyfinURL, testJellyfinToken) })

	c, err := NewPlayingCollector(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ch := make(chan prometheus.Metric, 10)
	if err := c.Update(ch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(ch)

	metrics := 0
	for range ch {
		metrics++
	}
	if metrics != 2 {
		t.Fatalf("expected 2 metrics (state + progress) for audio, got %d", metrics)
	}
}
