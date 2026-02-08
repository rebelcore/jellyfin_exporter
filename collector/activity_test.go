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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
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
		_, _ = w.Write([]byte(`[{"user_id":"1","user_name":"Bob","total_count":3,"last_seen":" 2025-01-01 ","total_play_time":" 01:00:00 "}]`))
	}))
	defer srv.Close()

	list, err := getUserActivity(srv.URL, token, "7")
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
}

func TestGetUserActivity_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer srv.Close()

	_, err := getUserActivity(srv.URL, "token", "1")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestActivityCollectorUpdate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := NewActivityCollector(logger)
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
	if metrics != 1 {
		t.Fatalf("expected 1 metric, got %d", metrics)
	}
}
