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

package collector

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

const testJellyfinToken = "test-token"

var testJellyfinURL string

func TestMain(m *testing.M) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "MediaBrowser Token="+testJellyfinToken, r.Header.Get("Authorization"); want != have {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/System/Ping":
			_, _ = w.Write([]byte("Jellyfin Server"))
		case "/System/Info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"ServerName":"Test Server",
				"Version":"1.0.0",
				"ProductName":"Jellyfin",
				"PackageName":"jellyfin",
				"Id":"server-1",
				"HasPendingRestart":true
			}`))
		case "/System/Info/Storage":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"ProgramDataFolder":{"FreeSpace":100,"UsedSpace":50},
				"TranscodingTempFolder":{"FreeSpace":200,"UsedSpace":25},
				"Libraries":[
					{"Id":"lib-1","Name":"Movies","Folders":[{"FreeSpace":300,"UsedSpace":10}]}
				]
			}`))
		case "/Items/Counts":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"MovieCount":10,"SeriesCount":2}`))
		case "/user_usage_stats/user_activity":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"user_id":"1","user_name":"Bob","total_count":3,"last_seen":" 2025-01-01 ","total_play_time":" 01:00:00 "}]`))
		case "/ScheduledTasks":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"Name":"Scan media library",
					"Category":"Library",
					"State":"Running",
					"CurrentProgressPercentage":12.5,
					"LastExecutionResult":{
						"StartTimeUtc":"2025-01-01T00:00:00Z",
						"EndTimeUtc":"2025-01-01T00:01:30Z",
						"Status":"Completed"
					}
				},
				{
					"Name":"Bad timestamps",
					"Category":"Other",
					"State":"Idle",
					"LastExecutionResult":{
						"StartTimeUtc":"nope",
						"EndTimeUtc":"nope",
						"Status":"Failed"
					}
				}
			]`))
		case "/Users":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"Name":"Alice","Id":"u1","LastActivityDate":"2025-01-01T00:00:00Z","Policy":{"IsDisabled":false,"IsAdministrator":true,"EnabledFolders":["f1"]}},
				{"Name":"Bob","Id":"u2","LastActivityDate":"","Policy":{"IsDisabled":true,"IsAdministrator":false,"EnabledFolders":[]}}
			]`))
		case "/Sessions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{
					"Id":"s1",
					"UserId":"u1",
					"UserName":"Alice",
					"DeviceName":"TV",
					"Client":"Web",
					"ApplicationVersion":"1.0",
					"RemoteEndPoint":"1.2.3.4",
					"PlayState":{"IsPaused":false,"PlayMethod":"DirectPlay","PositionTicks":50000000},
					"NowPlayingItem":{"Type":"Movie","Name":"Title","RunTimeTicks":100000000}
				},
				{
					"Id":"s2",
					"UserId":"u2",
					"UserName":"Bob",
					"DeviceName":"Tablet",
					"Client":"Web",
					"ApplicationVersion":"1.0",
					"RemoteEndPoint":"5.6.7.8",
					"PlayState":{"IsPaused":true,"PlayMethod":"Transcode"},
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
						"TranscodeReasons":["ContainerNotSupported","", "VideoCodecNotSupported"]
					}
				}
			]`))
		default:
			logger.Error("unexpected mock path", "path", r.URL.Path)
			http.NotFound(w, r)
		}
	}))

	testJellyfinURL = srv.URL
	_, err := kingpin.CommandLine.Parse([]string{
		"--jellyfin.address", testJellyfinURL,
		"--jellyfin.token", testJellyfinToken,
	})
	if err != nil {
		logger.Error("failed to parse kingpin flags", "err", err)
		srv.Close()
		os.Exit(2)
	}

	code := m.Run()
	srv.Close()
	os.Exit(code)
}

func setJellyfinFlags(t *testing.T, url, token string) {
	t.Helper()

	_, err := kingpin.CommandLine.Parse([]string{
		"--jellyfin.address", url,
		"--jellyfin.token", token,
	})
	if err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}
}

type stubCollector struct {
	err error
}

func (s stubCollector) Update(ch chan<- prometheus.Metric) error {
	return s.err
}

func TestDisableDefaultCollectors_RespectsForcedCollectors(t *testing.T) {
	prevForced := make(map[string]bool, len(forcedCollectors))
	for k, v := range forcedCollectors {
		prevForced[k] = v
	}
	defer func() { forcedCollectors = prevForced }()

	prev := make(map[string]bool, len(collectorState))
	for name, flag := range collectorState {
		prev[name] = *flag
		*flag = true
	}
	defer func() {
		for name, flag := range collectorState {
			if v, ok := prev[name]; ok {
				*flag = v
			}
		}
	}()

	forcedCollectors = map[string]bool{"media": true}
	DisableDefaultCollectors()

	for name, flag := range collectorState {
		if name == "media" {
			if !*flag {
				t.Fatalf("expected %q to remain enabled", name)
			}
			continue
		}
		if *flag {
			t.Fatalf("expected %q to be disabled", name)
		}
	}
}

func TestCollectorFlagAction_TracksForcedCollectors(t *testing.T) {
	prev := make(map[string]bool, len(forcedCollectors))
	for k, v := range forcedCollectors {
		prev[k] = v
	}
	defer func() { forcedCollectors = prev }()

	forcedCollectors = map[string]bool{}
	if err := collectorFlagAction("media")(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !forcedCollectors["media"] {
		t.Fatalf("expected media to be forced")
	}
}

func TestNewJellyfinCollector_FilterErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prev := make(map[string]bool, len(collectorState))
	for name, flag := range collectorState {
		prev[name] = *flag
	}
	defer func() {
		for name, flag := range collectorState {
			*flag = prev[name]
		}
	}()

	*collectorState["activity"] = false
	*collectorState["media"] = true

	if _, err := NewJellyfinCollector(logger, "does_not_exist"); err == nil {
		t.Fatalf("expected error for missing collector")
	}
	if _, err := NewJellyfinCollector(logger, "activity"); err == nil || !strings.Contains(err.Error(), "disabled collector") {
		t.Fatalf("expected disabled collector error, got %v", err)
	}
	got, err := NewJellyfinCollector(logger, "media")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Collectors) != 1 {
		t.Fatalf("expected 1 collector, got %d", len(got.Collectors))
	}
	if _, ok := got.Collectors["media"]; !ok {
		t.Fatalf("expected media collector to be enabled")
	}
}

func TestNewJellyfinCollector_ReusesInitiatedCollector(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevFactories := make(map[string]func(*slog.Logger) (Collector, error), len(factories))
	for k, v := range factories {
		prevFactories[k] = v
	}
	defer func() { factories = prevFactories }()

	prevState := make(map[string]bool, len(collectorState))
	for name, flag := range collectorState {
		prevState[name] = *flag
	}
	defer func() {
		for name, flag := range collectorState {
			*flag = prevState[name]
		}
	}()

	initiatedCollectorsMtx.Lock()
	prevInitiated := initiatedCollectors
	initiatedCollectors = map[string]Collector{}
	initiatedCollectorsMtx.Unlock()
	defer func() {
		initiatedCollectorsMtx.Lock()
		initiatedCollectors = prevInitiated
		initiatedCollectorsMtx.Unlock()
	}()

	*collectorState["media"] = true

	first, err := NewJellyfinCollector(logger, "media")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := first.Collectors["media"]; !ok {
		t.Fatalf("expected media collector")
	}

	factories["media"] = func(*slog.Logger) (Collector, error) {
		return nil, io.ErrUnexpectedEOF
	}

	second, err := NewJellyfinCollector(logger, "media")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := second.Collectors["media"]; !ok {
		t.Fatalf("expected media collector")
	}
}

func TestNewJellyfinCollector_FactoryError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevFactory := factories["media"]
	defer func() { factories["media"] = prevFactory }()

	initiatedCollectorsMtx.Lock()
	prevInitiated := initiatedCollectors
	initiatedCollectors = map[string]Collector{}
	initiatedCollectorsMtx.Unlock()
	defer func() {
		initiatedCollectorsMtx.Lock()
		initiatedCollectors = prevInitiated
		initiatedCollectorsMtx.Unlock()
	}()

	prev := *collectorState["media"]
	*collectorState["media"] = true
	defer func() { *collectorState["media"] = prev }()

	factories["media"] = func(*slog.Logger) (Collector, error) {
		return nil, io.ErrUnexpectedEOF
	}

	if _, err := NewJellyfinCollector(logger, "media"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestJellyfinCollector_DescribeAndCollect(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := JellyfinCollector{
		Collectors: map[string]Collector{
			"stub": stubCollector{err: nil},
		},
		logger: logger,
	}

	descCh := make(chan *prometheus.Desc, 10)
	c.Describe(descCh)
	close(descCh)
	var descs []*prometheus.Desc
	for d := range descCh {
		descs = append(descs, d)
	}
	if len(descs) != 2 {
		t.Fatalf("expected 2 descs, got %d", len(descs))
	}

	metricCh := make(chan prometheus.Metric, 10)
	c.Collect(metricCh)
	close(metricCh)
	var metrics []prometheus.Metric
	for m := range metricCh {
		metrics = append(metrics, m)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics from execute, got %d", len(metrics))
	}
}

func TestExecute_SetsSuccessGauge(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("success", func(t *testing.T) {
		ch := make(chan prometheus.Metric, 10)
		execute("ok", stubCollector{err: nil}, ch, logger)
		close(ch)

		var gotSuccess *dto.Metric
		for m := range ch {
			if !strings.Contains(m.Desc().String(), "collector_success") {
				continue
			}
			var dm dto.Metric
			if err := m.Write(&dm); err != nil {
				t.Fatalf("write metric: %v", err)
			}
			gotSuccess = &dm
		}
		if gotSuccess == nil || gotSuccess.Gauge == nil {
			t.Fatalf("expected success gauge metric")
		}
		if want, have := 1.0, gotSuccess.Gauge.GetValue(); want != have {
			t.Fatalf("want %v, have %v", want, have)
		}
	})

	t.Run("no_data", func(t *testing.T) {
		ch := make(chan prometheus.Metric, 10)
		execute("nodata", stubCollector{err: ErrNoData}, ch, logger)
		close(ch)

		var gotSuccess *dto.Metric
		for m := range ch {
			if !strings.Contains(m.Desc().String(), "collector_success") {
				continue
			}
			var dm dto.Metric
			if err := m.Write(&dm); err != nil {
				t.Fatalf("write metric: %v", err)
			}
			gotSuccess = &dm
		}
		if gotSuccess == nil || gotSuccess.Gauge == nil {
			t.Fatalf("expected success gauge metric")
		}
		if want, have := 0.0, gotSuccess.Gauge.GetValue(); want != have {
			t.Fatalf("want %v, have %v", want, have)
		}
	})

	t.Run("error", func(t *testing.T) {
		ch := make(chan prometheus.Metric, 10)
		execute("err", stubCollector{err: io.ErrUnexpectedEOF}, ch, logger)
		close(ch)

		var gotSuccess *dto.Metric
		for m := range ch {
			if !strings.Contains(m.Desc().String(), "collector_success") {
				continue
			}
			var dm dto.Metric
			if err := m.Write(&dm); err != nil {
				t.Fatalf("write metric: %v", err)
			}
			gotSuccess = &dm
		}
		if gotSuccess == nil || gotSuccess.Gauge == nil {
			t.Fatalf("expected success gauge metric")
		}
		if want, have := 0.0, gotSuccess.Gauge.GetValue(); want != have {
			t.Fatalf("want %v, have %v", want, have)
		}
	})
}

func TestIsNoDataError(t *testing.T) {
	if !IsNoDataError(ErrNoData) {
		t.Fatalf("expected ErrNoData to be recognized")
	}
	if IsNoDataError(io.EOF) {
		t.Fatalf("expected io.EOF not to be recognized")
	}
}

func TestCollectorsUpdate_ReturnErrorsOnHTTPFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	setJellyfinFlags(t, srv.URL, testJellyfinToken)
	t.Cleanup(func() { setJellyfinFlags(t, testJellyfinURL, testJellyfinToken) })

	tests := []struct {
		name string
		new  func(*slog.Logger) (Collector, error)
	}{
		{name: "activity", new: NewActivityCollector},
		{name: "media", new: NewMediaCollector},
		{name: "playing", new: NewPlayingCollector},
		{name: "storage", new: NewStorageCollector},
		{name: "system", new: NewSystemCollector},
		{name: "tasks", new: NewTasksCollector},
		//{name: "transcoding", new: NewTranscodingCollector},
		{name: "users", new: NewUsersCollector},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := tt.new(logger)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			ch := make(chan prometheus.Metric, 10)
			if err := c.Update(ch); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestCollectorsUpdate_ReturnErrorsOnConfigFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	setJellyfinFlags(t, testJellyfinURL, "")
	t.Cleanup(func() { setJellyfinFlags(t, testJellyfinURL, testJellyfinToken) })

	tests := []struct {
		name string
		new  func(*slog.Logger) (Collector, error)
	}{
		{name: "activity", new: NewActivityCollector},
		{name: "media", new: NewMediaCollector},
		{name: "playing", new: NewPlayingCollector},
		{name: "storage", new: NewStorageCollector},
		{name: "system", new: NewSystemCollector},
		{name: "tasks", new: NewTasksCollector},
		//{name: "transcoding", new: NewTranscodingCollector},
		{name: "users", new: NewUsersCollector},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := tt.new(logger)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			ch := make(chan prometheus.Metric, 10)
			if err := c.Update(ch); err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestUsersCollectorUpdate_PartialErrorStillEmitsSessions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Users":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/Sessions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"UserId":"u1","UserName":"Alice","Client":"Web","ApplicationVersion":"1","DeviceName":"iPad","RemoteEndPoint":"1.2.3.4"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	setJellyfinFlags(t, srv.URL, testJellyfinToken)
	t.Cleanup(func() { setJellyfinFlags(t, testJellyfinURL, testJellyfinToken) })

	c, err := NewUsersCollector(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ch := make(chan prometheus.Metric, 10)
	err = c.Update(ch)
	close(ch)
	if err == nil {
		t.Fatalf("expected error")
	}

	metrics := 0
	for range ch {
		metrics++
	}
	if metrics != 1 {
		t.Fatalf("expected 1 session metric, got %d", metrics)
	}
}
