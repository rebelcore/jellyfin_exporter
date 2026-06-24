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

package main

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"strings"
	"sync"
	"testing"

	"github.com/alecthomas/kingpin/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"

	"github.com/rebelcore/jellyfin_exporter/collector"
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
		case "/Items/Counts":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"MovieCount":10,"SeriesCount":2}`))
		case "/Users":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"Name":"Alice","Id":"u1","LastActivityDate":"2025-01-01T00:00:00Z","Policy":{"IsDisabled":false,"IsAdministrator":true,"EnabledFolders":["f1"]}}
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

func TestHandler_ServeHTTP_OK(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevNewCollector := newJellyfinCollector
	t.Cleanup(func() { newJellyfinCollector = prevNewCollector })

	for _, includeExporterMetrics := range []bool{false, true} {
		t.Run("include_exporter_metrics="+strconvBool(includeExporterMetrics), func(t *testing.T) {
			h, err := newHandler(includeExporterMetrics, 0, logger)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://example/metrics", nil)
			h.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("want status %d, have %d. Body:\n%s", http.StatusOK, rr.Code, rr.Body.String())
			}

			body := rr.Body.String()
			if !strings.Contains(body, "jellyfin_up") {
				t.Fatalf("expected jellyfin_up in response body")
			}
			if !strings.Contains(body, "jellyfin_scrape_collector_success") {
				t.Fatalf("expected scrape metrics in response body")
			}
		})
	}
}

func TestHandler_ServeHTTP_RejectsCombinedCollectExclude(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	prevNewCollector := newJellyfinCollector
	t.Cleanup(func() { newJellyfinCollector = prevNewCollector })
	h, err := newHandler(false, 0, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example/metrics?collect[]=system&exclude[]=media", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want status %d, have %d. Body:\n%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
}

func TestHandler_ServeHTTP_MissingCollector(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	prevNewCollector := newJellyfinCollector
	t.Cleanup(func() { newJellyfinCollector = prevNewCollector })
	h, err := newHandler(false, 0, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example/metrics?collect[]=does_not_exist", nil)
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want status %d, have %d. Body:\n%s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
}

func TestHandler_ServeHTTP_CollectAndExclude(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	prevNewCollector := newJellyfinCollector
	t.Cleanup(func() { newJellyfinCollector = prevNewCollector })
	h, err := newHandler(false, 0, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Run("collect", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example/metrics?collect[]=system", nil)
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("want status %d, have %d. Body:\n%s", http.StatusOK, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "jellyfin_system_info") {
			t.Fatalf("expected system metrics in response body")
		}
	})

	t.Run("exclude", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example/metrics?exclude[]=system", nil)
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("want status %d, have %d. Body:\n%s", http.StatusOK, rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), "jellyfin_system_info") {
			t.Fatalf("did not expect system metrics in response body")
		}
	})
}

func TestNewHandler_InnerHandlerError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prev := newJellyfinCollector
	t.Cleanup(func() { newJellyfinCollector = prev })

	newJellyfinCollector = func(*slog.Logger, ...string) (*collector.JellyfinCollector, error) {
		return nil, errors.New("boom")
	}

	if _, err := newHandler(false, 0, logger); err == nil {
		t.Fatalf("expected error")
	}
}

func TestNewHandler_RegisterError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prev := registerWithRegistry
	t.Cleanup(func() { registerWithRegistry = prev })

	registerWithRegistry = func(*prometheus.Registry, prometheus.Collector) error {
		return errors.New("boom")
	}

	if _, err := newHandler(false, 0, logger); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_BuildsMuxAndCallsListenAndServe(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevListen := listenAndServe
	prevNewLandingPage := newLandingPage
	t.Cleanup(func() { listenAndServe = prevListen })
	t.Cleanup(func() { newLandingPage = prevNewLandingPage })

	called := false
	listenAndServe = func(server *http.Server, _ *web.FlagConfig, _ *slog.Logger) error {
		called = true

		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example/metrics", nil)
		server.Handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("want status %d, have %d. Body:\n%s", http.StatusOK, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "jellyfin_up") {
			t.Fatalf("expected jellyfin_up in response body")
		}

		rr = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodGet, "http://example/", nil)
		server.Handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("want status %d, have %d. Body:\n%s", http.StatusOK, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "Jellyfin Exporter") {
			t.Fatalf("expected landing page content")
		}

		return nil
	}

	flags := &web.FlagConfig{
		WebListenAddresses: ptr([]string{":0"}),
		WebSystemdSocket:   ptr(false),
		WebConfigFile:      ptr(""),
	}

	if err := run("/metrics", false, 0, false, 1, false, flags, logger); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatalf("expected listenAndServe to be called")
	}
}

func TestRun_MetricsPathRoot(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevListen := listenAndServe
	prevNewLandingPage := newLandingPage
	t.Cleanup(func() { listenAndServe = prevListen })
	t.Cleanup(func() { newLandingPage = prevNewLandingPage })

	listenAndServe = func(server *http.Server, _ *web.FlagConfig, _ *slog.Logger) error {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
		server.Handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("want status %d, have %d. Body:\n%s", http.StatusOK, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "jellyfin_up") {
			t.Fatalf("expected jellyfin_up in response body")
		}
		return nil
	}

	flags := &web.FlagConfig{
		WebListenAddresses: ptr([]string{":0"}),
		WebSystemdSocket:   ptr(false),
		WebConfigFile:      ptr(""),
	}

	if err := run("/", false, 0, false, 1, false, flags, logger); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_RootUserBranch(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevListen := listenAndServe
	prevCurrentUser := currentUser
	prevNewLandingPage := newLandingPage
	t.Cleanup(func() {
		listenAndServe = prevListen
		currentUser = prevCurrentUser
		newLandingPage = prevNewLandingPage
	})

	currentUser = func() (*user.User, error) {
		return &user.User{Uid: "0"}, nil
	}
	listenAndServe = func(_ *http.Server, _ *web.FlagConfig, _ *slog.Logger) error { return nil }

	flags := &web.FlagConfig{
		WebListenAddresses: ptr([]string{":0"}),
		WebSystemdSocket:   ptr(false),
		WebConfigFile:      ptr(""),
	}

	if err := run("/metrics", false, 0, false, 1, false, flags, logger); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_ListenAndServeError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevListen := listenAndServe
	prevNewLandingPage := newLandingPage
	t.Cleanup(func() { listenAndServe = prevListen })
	t.Cleanup(func() { newLandingPage = prevNewLandingPage })

	listenAndServe = func(_ *http.Server, _ *web.FlagConfig, _ *slog.Logger) error {
		return errors.New("boom")
	}

	flags := &web.FlagConfig{
		WebListenAddresses: ptr([]string{":0"}),
		WebSystemdSocket:   ptr(false),
		WebConfigFile:      ptr(""),
	}

	if err := run("/metrics", false, 0, false, 1, false, flags, logger); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_NilToolkitFlags(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := run("/metrics", false, 0, false, 1, false, nil, logger); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_DisableDefaultCollectorsBranch(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevListen := listenAndServe
	prevNewLandingPage := newLandingPage
	t.Cleanup(func() { listenAndServe = prevListen })
	t.Cleanup(func() { newLandingPage = prevNewLandingPage })
	listenAndServe = func(_ *http.Server, _ *web.FlagConfig, _ *slog.Logger) error { return nil }

	flags := &web.FlagConfig{
		WebListenAddresses: ptr([]string{":0"}),
		WebSystemdSocket:   ptr(false),
		WebConfigFile:      ptr(""),
	}

	if err := run("/metrics", false, 0, true, 1, false, flags, logger); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_NewHandlerError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevListen := listenAndServe
	prevNewCollector := newJellyfinCollector
	t.Cleanup(func() {
		listenAndServe = prevListen
		newJellyfinCollector = prevNewCollector
	})

	listenAndServe = func(*http.Server, *web.FlagConfig, *slog.Logger) error {
		t.Fatalf("listenAndServe should not be called")
		return nil
	}
	newJellyfinCollector = func(*slog.Logger, ...string) (*collector.JellyfinCollector, error) {
		return nil, errors.New("boom")
	}

	flags := &web.FlagConfig{
		WebListenAddresses: ptr([]string{":0"}),
		WebSystemdSocket:   ptr(false),
		WebConfigFile:      ptr(""),
	}

	if err := run("/metrics", false, 0, false, 1, false, flags, logger); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRun_BuildMuxError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevListen := listenAndServe
	prevNewLandingPage := newLandingPage
	t.Cleanup(func() {
		listenAndServe = prevListen
		newLandingPage = prevNewLandingPage
	})

	listenAndServe = func(*http.Server, *web.FlagConfig, *slog.Logger) error {
		t.Fatalf("listenAndServe should not be called")
		return nil
	}
	newLandingPage = func(web.LandingConfig) (*web.LandingPageHandler, error) {
		return nil, errors.New("boom")
	}

	flags := &web.FlagConfig{
		WebListenAddresses: ptr([]string{":0"}),
		WebSystemdSocket:   ptr(false),
		WebConfigFile:      ptr(""),
	}

	if err := run("/metrics", false, 0, false, 1, false, flags, logger); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildMux_LandingPageError(t *testing.T) {
	prev := newLandingPage
	t.Cleanup(func() { newLandingPage = prev })

	newLandingPage = func(web.LandingConfig) (*web.LandingPageHandler, error) {
		return nil, errors.New("boom")
	}

	_, err := buildMux("/metrics", http.NewServeMux(), false)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandler_InnerHandlerEnabledCollectorsOnce(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevNewCollector := newJellyfinCollector
	t.Cleanup(func() { newJellyfinCollector = prevNewCollector })

	// Return a fixed collector set regardless of filters, so the test does not
	// depend on the (global, mutable) collector enable/disable state.
	newJellyfinCollector = func(*slog.Logger, ...string) (*collector.JellyfinCollector, error) {
		return &collector.JellyfinCollector{
			Collectors: map[string]collector.Collector{"alpha": nil, "beta": nil, "gamma": nil},
		}, nil
	}

	// newHandler runs innerHandler once with no filters, populating the full
	// enabled-collector set.
	h, err := newHandler(false, 0, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := len(h.enabledCollectors)
	if want == 0 {
		t.Fatalf("expected enabledCollectors to be populated")
	}

	// A request that excludes every collector resolves to an empty filter set,
	// driving innerHandler down the no-filter branch again. Hit it concurrently:
	// without the sync.Once guard this races on, and duplicates entries in,
	// h.enabledCollectors (caught here by -race and the length assertion).
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := h.innerHandler(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := len(h.enabledCollectors); got != want {
		t.Fatalf("enabledCollectors changed: want %d entries, got %d (%v)", want, got, h.enabledCollectors)
	}
}

func TestBuildMux_Pprof(t *testing.T) {
	const pprofMarker = "Types of profiles available"

	for _, enabled := range []bool{false, true} {
		t.Run("enabled="+strconvBool(enabled), func(t *testing.T) {
			mux, err := buildMux("/metrics", http.NewServeMux(), enabled)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://example/debug/pprof/", nil)
			mux.ServeHTTP(rr, req)

			hasPprof := strings.Contains(rr.Body.String(), pprofMarker)
			if enabled && !hasPprof {
				t.Fatalf("pprof enabled: expected pprof index at /debug/pprof/, got status %d body:\n%s", rr.Code, rr.Body.String())
			}
			if !enabled && hasPprof {
				t.Fatalf("pprof disabled: did not expect pprof index to be served")
			}
		})
	}
}

func TestBuildMux_LandingPageProfilingFollowsFlag(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run("enabled="+strconvBool(enabled), func(t *testing.T) {
			mux, err := buildMux("/metrics", http.NewServeMux(), enabled)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://example/", nil)
			mux.ServeHTTP(rr, req)
			if rr.Code != http.StatusOK {
				t.Fatalf("want status %d for landing page, got %d", http.StatusOK, rr.Code)
			}

			// The pprof links on the landing page must only appear when the
			// endpoints are actually registered, so they never 404.
			hasProfilingLinks := strings.Contains(rr.Body.String(), "debug/pprof/heap")
			if enabled && !hasProfilingLinks {
				t.Fatalf("pprof enabled: expected profiling links on the landing page")
			}
			if !enabled && hasProfilingLinks {
				t.Fatalf("pprof disabled: did not expect profiling links on the landing page")
			}
		})
	}
}

func strconvBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func ptr[T any](v T) *T { return &v }

func TestGitTag(t *testing.T) {
	prev := version.Version
	t.Cleanup(func() { version.Version = prev })

	version.Version = ""
	if got := gitTag(); got != "unknown" {
		t.Fatalf("want %q, got %q", "unknown", got)
	}

	version.Version = "1.2.3"
	if got := gitTag(); got != "v1.2.3" {
		t.Fatalf("want %q, got %q", "v1.2.3", got)
	}

	version.Version = "v1.2.3"
	if got := gitTag(); got != "v1.2.3" {
		t.Fatalf("want %q, got %q", "v1.2.3", got)
	}
}

func TestVersionString_IncludesGitTag(t *testing.T) {
	prevVersion := version.Version
	prevBranch := version.Branch
	prevRevision := version.Revision
	t.Cleanup(func() {
		version.Version = prevVersion
		version.Branch = prevBranch
		version.Revision = prevRevision
	})

	version.Version = "1.2.3"
	version.Branch = "master"
	version.Revision = "deadbeef"

	s := versionString("jellyfin_exporter")
	if !strings.Contains(s, "git=v1.2.3") {
		t.Fatalf("expected version output to include git tag, got:\n%s", s)
	}
}
