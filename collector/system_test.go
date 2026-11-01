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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

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

// TestSystemCollectorUpdate_EmitsUpZeroWhenPingFails guards the liveness
// contract: an unreachable server must report jellyfin_up 0, not omit the
// series, or alerts written against `jellyfin_up == 0` never fire.
func TestSystemCollectorUpdate_EmitsUpZeroWhenPingFails(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	setJellyfinFlags(t, srv.URL, testJellyfinToken)
	t.Cleanup(func() { setJellyfinFlags(t, testJellyfinURL, testJellyfinToken) })

	c, err := NewSystemCollector(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ch := make(chan prometheus.Metric, 10)
	updateErr := c.Update(ch)
	close(ch)

	if updateErr == nil {
		t.Fatalf("expected Update to report the failure")
	}

	up, found := jellyfinUp(t, ch)
	if !found {
		t.Fatalf("expected jellyfin_up to be emitted when the server is unreachable")
	}
	if want, have := 0.0, up; want != have {
		t.Fatalf("want jellyfin_up %v, have %v", want, have)
	}
}

// TestSystemCollectorUpdate_EmitsUpZeroWhenConfigFails extends the liveness
// contract to connection settings that stop working after startup, such as a
// token file that has been removed: jellyfin_up must still be emitted, as 0.
func TestSystemCollectorUpdate_EmitsUpZeroWhenConfigFails(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(testJellyfinToken), 0o600); err != nil {
		t.Fatalf("writing the token file: %v", err)
	}
	if _, err := parseFlags("--jellyfin.address", testJellyfinURL, "--jellyfin.token-file", path); err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}
	t.Cleanup(func() { setJellyfinFlags(t, testJellyfinURL, testJellyfinToken) })
	if err := os.Remove(path); err != nil {
		t.Fatalf("removing the token file: %v", err)
	}

	c, err := NewSystemCollector(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ch := make(chan prometheus.Metric, 10)
	updateErr := c.Update(ch)
	close(ch)

	if updateErr == nil {
		t.Fatalf("expected Update to report the failure")
	}

	up, found := jellyfinUp(t, ch)
	if !found {
		t.Fatalf("expected jellyfin_up to be emitted when the token file can't be read")
	}
	if want, have := 0.0, up; want != have {
		t.Fatalf("want jellyfin_up %v, have %v", want, have)
	}
}

// jellyfinUp drains ch and returns the value of the jellyfin_up metric it
// carried, and whether it carried one.
func jellyfinUp(t *testing.T, ch <-chan prometheus.Metric) (float64, bool) {
	t.Helper()

	var (
		value float64
		found bool
	)
	for m := range ch {
		if !strings.Contains(m.Desc().String(), "jellyfin_up") {
			continue
		}
		var dm dto.Metric
		if err := m.Write(&dm); err != nil {
			t.Fatalf("write metric: %v", err)
		}
		value, found = dm.GetGauge().GetValue(), true
	}
	return value, found
}
