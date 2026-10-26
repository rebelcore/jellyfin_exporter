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

//go:build !noactivity

package collector

import (
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestGetUserActivity(t *testing.T) {
	const token = "abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "/user_usage_stats/user_activity", r.URL.Path; want != have {
			t.Fatalf("want path %q, have %q", want, have)
		}
		if want, have := "MediaBrowser Token="+token, r.Header.Get("Authorization"); want != have {
			t.Fatalf("want Authorization header %q, have %q", want, have)
		}
		if want, have := "7", r.URL.Query().Get("days"); want != have {
			t.Fatalf("want days=%q, have %q", want, have)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"user_id":"1","user_name":"Bob","total_count":3,"total_time":3725,"last_seen":"2 hours 5 minutes ","total_play_time":"1 hour 2 minutes "}]`))
	}))
	defer srv.Close()

	list, err := getUserActivity(srv.URL, token, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 activity, got %d", len(list))
	}
	if want, have := "Bob", list[0].UserName; want != have {
		t.Fatalf("want username %q, have %q", want, have)
	}
	if want, have := 3.0, list[0].TotalCount; want != have {
		t.Fatalf("want total_count %v, have %v", want, have)
	}
	if want, have := 3725.0, list[0].TotalTime; want != have {
		t.Fatalf("want total_time %v, have %v", want, have)
	}
}

func TestGetUserActivity_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer srv.Close()

	_, err := getUserActivity(srv.URL, "token", 1)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestParseLastSeen(t *testing.T) {
	const (
		minute = 60
		hour   = 60 * minute
		day    = 24 * hour
	)

	tests := []struct {
		text string
		want float64
	}{
		{text: "just now", want: 0},
		{text: "1 minute ", want: minute},
		{text: "2 hours 5 minutes ", want: 2*hour + 5*minute},
		{text: "1 week ", want: 7 * day},
		{text: "1 year 2 weeks 3 days 4 hours 5 minutes ", want: 365*day + 2*7*day + 3*day + 4*hour + 5*minute},
	}
	for _, tt := range tests {
		have, err := parseLastSeen(tt.text)
		if err != nil {
			t.Errorf("parseLastSeen(%q): unexpected error: %v", tt.text, err)
			continue
		}
		if have != tt.want {
			t.Errorf("parseLastSeen(%q): want %v, have %v", tt.text, tt.want, have)
		}
	}

	for _, text := range []string{"", "2025-01-01", "5", "5 fortnights", "-1 minutes", "two minutes", "< 1 minute"} {
		if _, err := parseLastSeen(text); err == nil {
			t.Errorf("parseLastSeen(%q): expected error", text)
		}
	}
}

// TestActivityCollectorUpdate pins each metric to one series per user, labeled
// by user alone, so a user's series carries on as the plugin's last_seen and
// total_play_time text changes.
func TestActivityCollectorUpdate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := NewActivityCollector(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	series := gatherActivity(t, c)

	for name, value := range map[string]float64{
		"jellyfin_activity_count":             3,
		"jellyfin_activity_play_time_seconds": 3725,
		"jellyfin_activity_last_seen_seconds": 7500,
	} {
		metrics := series[name]
		if len(metrics) != 1 {
			t.Fatalf("want 1 %s series, have %d", name, len(metrics))
		}
		labels := make(map[string]string)
		for _, lp := range metrics[0].GetLabel() {
			labels[lp.GetName()] = lp.GetValue()
		}
		if want, have := map[string]string{"user_id": "1", "username": "Bob"}, labels; !maps.Equal(want, have) {
			t.Errorf("%s: want labels %v, have %v", name, want, have)
		}
		if want, have := value, metrics[0].GetGauge().GetValue(); want != have {
			t.Errorf("%s: want %v, have %v", name, want, have)
		}
	}
}

func TestActivityCollectorUpdate_SkipsUnexpectedLastSeen(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"user_id":"1","user_name":"Bob","total_count":3,"total_time":3725,"last_seen":"2025-01-01"}]`))
	}))
	defer srv.Close()

	setJellyfinFlags(t, srv.URL, testJellyfinToken)
	t.Cleanup(func() { setJellyfinFlags(t, testJellyfinURL, testJellyfinToken) })

	c, err := NewActivityCollector(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	series := gatherActivity(t, c)

	if have := len(series["jellyfin_activity_last_seen_seconds"]); have != 0 {
		t.Errorf("want no jellyfin_activity_last_seen_seconds series, have %d", have)
	}
	for _, name := range []string{"jellyfin_activity_count", "jellyfin_activity_play_time_seconds"} {
		if have := len(series[name]); have != 1 {
			t.Errorf("want 1 %s series, have %d", name, have)
		}
	}
}

// gatherActivity scrapes c through a registry, as the exporter does, and
// returns the series by metric name. It fails the test if the collector did
// not succeed.
func gatherActivity(t *testing.T, c Collector) map[string][]*dto.Metric {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := prometheus.NewRegistry()
	reg.MustRegister(JellyfinCollector{Collectors: map[string]Collector{"activity": c}, logger: logger})

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather reported an error: %v", err)
	}
	series := make(map[string][]*dto.Metric, len(families))
	for _, mf := range families {
		series[mf.GetName()] = mf.GetMetric()
	}
	if success := series["jellyfin_scrape_collector_success"]; len(success) != 1 || success[0].GetGauge().GetValue() != 1 {
		t.Fatalf("activity collector did not succeed")
	}
	return series
}
