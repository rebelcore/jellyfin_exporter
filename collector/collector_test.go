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
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/rebelcore/jellyfin_exporter/config"
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
			_, _ = w.Write([]byte(`[{"user_id":"1","user_name":"Bob","total_count":3,"total_time":3725,"last_seen":"2 hours 5 minutes ","total_play_time":"1 hour 2 minutes "}]`))
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
	if _, err := parseFlags("--jellyfin.address", testJellyfinURL, "--jellyfin.token", testJellyfinToken); err != nil {
		logger.Error("failed to parse flags", "err", err)
		srv.Close()
		os.Exit(2)
	}

	code := m.Run()
	srv.Close()
	os.Exit(code)
}

// parseFlags parses args against the connection and collector flags, as the
// exporter's command line does, so every flag's default is applied too.
func parseFlags(args ...string) (*kong.Context, error) {
	options := []kong.Option{kong.Embed(config.Flags())}
	for _, group := range Flags() {
		options = append(options, kong.Embed(group))
	}
	parser, err := kong.New(&struct{}{}, options...)
	if err != nil {
		return nil, err
	}
	return parser.Parse(args)
}

func setJellyfinFlags(t *testing.T, url, token string) {
	t.Helper()

	if _, err := parseFlags("--jellyfin.address", url, "--jellyfin.token", token); err != nil {
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

func TestFlags_TogglesUpdateCollectorState(t *testing.T) {
	t.Cleanup(SnapshotCollectorStates())
	t.Cleanup(func() { setJellyfinFlags(t, testJellyfinURL, testJellyfinToken) })

	if _, err := parseFlags("--jellyfin.token", "token", "--collector.transcoding", "--no-collector.users"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// transcoding is off by default and users on, so both flags flip them.
	for name, enabled := range collectorState {
		want := collectorDefaults[name]
		switch name {
		case "transcoding":
			want = true
		case "users":
			want = false
		}
		if *enabled != want {
			t.Errorf("collector %q: want enabled=%t, have %t", name, want, *enabled)
		}
	}
	if want, have := 36525, activityFlags.Days; want != have {
		t.Errorf("want --collector.activity.days default %d, have %d", want, have)
	}
}

func TestMarkForcedCollectors_TracksFlagsGivenOnTheCommandLine(t *testing.T) {
	t.Cleanup(SnapshotCollectorStates())
	t.Cleanup(func() { setJellyfinFlags(t, testJellyfinURL, testJellyfinToken) })

	clear(forcedCollectors)
	ctx, err := parseFlags("--jellyfin.token", "token", "--collector.media", "--no-collector.users", "--collector.activity.days", "7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	MarkForcedCollectors(ctx)

	// Both toggles count, whichever way they point; collector options such as
	// --collector.activity.days do not.
	if want := map[string]bool{"media": true, "users": true}; !maps.Equal(forcedCollectors, want) {
		t.Fatalf("want forced collectors %v, have %v", want, forcedCollectors)
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
		{name: "transcoding", new: NewTranscodingCollector},
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
		{name: "transcoding", new: NewTranscodingCollector},
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

// FuzzCollectors serves the same arbitrary bytes for every Jellyfin API request
// and runs each registered collector against them. However malformed the
// response, Update must return an error rather than panic, since a panic would
// take down the whole scrape.
func FuzzCollectors(f *testing.F) {
	for _, seed := range []string{
		`[]`,
		`{}`,
		`null`,
		`{"not":"an array"}`,
		`[{"Id":"s1","UserId":"u1","UserName":"Alice","PlayState":{"PositionTicks":-5,"IsPaused":true,"PlayMethod":"Transcode"},"NowPlayingItem":{"Name":"Title","Type":"Episode","SeriesName":"Show","ParentIndexNumber":1,"IndexNumber":2,"RunTimeTicks":0},"TranscodingInfo":{"IsAudioDirect":false,"TranscodeReasons":[" ","AudioCodecNotSupported"]}}]`,
		`[{"PlayState":null,"NowPlayingItem":null,"TranscodingInfo":null}]`,
		`[{"Name":"Bob","Id":"u2","LastActivityDate":"0001-01-01T00:00:00Z","Policy":{"IsAdministrator":true,"IsDisabled":true}}]`,
		`[{"user_id":"1","user_name":"Bob","total_count":3,"total_time":3725,"last_seen":"1 year 2 weeks 3 days 4 hours 5 minutes "}]`,
	} {
		f.Add([]byte(seed))
	}

	var body atomic.Pointer[[]byte]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(*body.Load())
	}))
	f.Cleanup(srv.Close)

	f.Cleanup(SnapshotCollectorStates())
	f.Cleanup(func() {
		if _, err := parseFlags("--jellyfin.address", testJellyfinURL, "--jellyfin.token", testJellyfinToken); err != nil {
			f.Errorf("failed to restore flags: %v", err)
		}
	})
	if _, err := parseFlags("--jellyfin.address", srv.URL, "--jellyfin.token", testJellyfinToken); err != nil {
		f.Fatalf("failed to parse flags: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	names := slices.Sorted(maps.Keys(factories))
	collectors := make([]Collector, len(names))
	for i, name := range names {
		c, err := factories[name](logger)
		if err != nil {
			f.Fatalf("%s: %v", name, err)
		}
		collectors[i] = c
	}

	f.Fuzz(func(_ *testing.T, b []byte) {
		body.Store(&b)
		for _, c := range collectors {
			ch := make(chan prometheus.Metric)
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				for range ch {
				}
			}()
			_ = c.Update(ch) // an error is expected for most inputs; a panic is not
			close(ch)
			<-drained
		}
	})
}
