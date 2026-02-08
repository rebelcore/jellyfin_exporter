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

//go:build !nosystem

package collector

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
)

func TestSystemUpValueFromPing(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want int
	}{
		{name: "raw string", body: []byte("Jellyfin Server"), want: 1},
		{name: "json string", body: []byte("\"Jellyfin Server\""), want: 1},
		{name: "whitespace", body: []byte("  Jellyfin Server \n"), want: 1},
		{name: "other", body: []byte("nope"), want: 0},
		{name: "empty", body: []byte(""), want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := utils.SystemUpValueFromPing(tt.body); got != tt.want {
				t.Fatalf("want %d, got %d", tt.want, got)
			}
		})
	}
}

func TestGetSystemPing(t *testing.T) {
	const token = "abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "/System/Ping", r.URL.Path; want != have {
			t.Fatalf("want path %q, have %q", want, have)
		}
		if want, have := "MediaBrowser Token="+token, r.Header.Get("Authorization"); want != have {
			t.Fatalf("want Authorization header %q, have %q", want, have)
		}
		_, _ = w.Write([]byte("Jellyfin Server"))
	}))
	defer srv.Close()

	got, err := getSystemPing(srv.URL, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 1 {
		t.Fatalf("want 1, got %v", got)
	}
}

func TestGetSystemInfo_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer srv.Close()

	_, err := getSystemInfo(srv.URL, "token")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestGetSystemInfo_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := getSystemInfo(srv.URL, "token")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestGetSystemInfo_InvalidURL(t *testing.T) {
	_, err := getSystemInfo(":", "token")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestSystemCollectorUpdate_GetSystemInfoError(t *testing.T) {
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
			_, _ = w.Write([]byte(`{`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	setJellyfinFlags(t, srv.URL, testJellyfinToken)
	t.Cleanup(func() { setJellyfinFlags(t, testJellyfinURL, testJellyfinToken) })

	c, err := NewSystemCollector(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ch := make(chan prometheus.Metric, 10)
	if err := c.Update(ch); err == nil {
		t.Fatalf("expected error")
	}
}

func TestSystemCollectorUpdate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := NewSystemCollector(logger)
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
	if metrics != 3 {
		t.Fatalf("expected 3 metrics, got %d", metrics)
	}
}
