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
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/user"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"

	"github.com/rebelcore/jellyfin_exporter/collector"
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
	if _, err := parseTestFlags("--jellyfin.address", testJellyfinURL, "--jellyfin.token", testJellyfinToken); err != nil {
		logger.Error("failed to parse flags", "err", err)
		srv.Close()
		os.Exit(2)
	}

	code := m.Run()
	srv.Close()
	os.Exit(code)
}

// parseTestFlags parses args through the exporter's command line, with exit
// stubbed out so --help and --version cannot end the test binary.
func parseTestFlags(args ...string) (*flags, error) {
	var f flags
	return &f, parseFlags(newParser(&f, kong.Exit(func(int) {})), args)
}

// restoreTestFlags undoes a test's parse once it finishes, restoring the
// collector state and the Jellyfin address and token TestMain set.
func restoreTestFlags(t *testing.T) {
	t.Helper()
	restoreCollectors := collector.SnapshotCollectorStates()
	t.Cleanup(func() {
		if _, err := parseTestFlags("--jellyfin.address", testJellyfinURL, "--jellyfin.token", testJellyfinToken); err != nil {
			t.Errorf("failed to restore flags: %v", err)
		}
		restoreCollectors()
	})
}

// clearFlagEnv empties the flags' environment variables for the test, so the
// environment the tests run in cannot leak into what they parse.
func clearFlagEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"JELLYFIN_ADDRESS", "JELLYFIN_TOKEN", "GOMAXPROCS"} {
		t.Setenv(name, "")
	}
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

	// run() with disableDefaultCollectors disables every collector for the rest
	// of the process, so restore the previous state or later tests find no
	// collectors enabled and fail depending on the order they run in.
	t.Cleanup(collector.SnapshotCollectorStates())

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

// TestHandler_ServeHTTP_ExcludeAllCollectors guards the exclude[] semantics: an
// empty filter list means "every enabled collector" to innerHandler, so a query
// that excludes them all must be rejected rather than silently serving the
// complete set — the exact inverse of what was asked for.
func TestHandler_ServeHTTP_ExcludeAllCollectors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	h, err := newHandler(false, 0, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h.enabledCollectors) == 0 {
		t.Fatalf("expected some enabled collectors")
	}

	query := url.Values{}
	for _, c := range h.enabledCollectors {
		query.Add("exclude[]", c)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics?"+query.Encode(), nil))

	if want, have := http.StatusBadRequest, rec.Code; want != have {
		t.Fatalf("want status %d, have %d. Body:\n%s", want, have, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "jellyfin_up") {
		t.Fatalf("excluding every collector still served collector metrics:\n%s", rec.Body.String())
	}
}

// TestValidateMetricsPath covers the values http.ServeMux would panic on, which
// would otherwise crash the exporter at startup instead of returning an error.
func TestValidateMetricsPath(t *testing.T) {
	for _, tc := range []struct {
		name        string
		path        string
		enablePprof bool
		wantErr     bool
	}{
		{name: "default", path: "/metrics"},
		{name: "root", path: "/"},
		{name: "nested", path: "/a/b/metrics"},
		{name: "pprof disabled allows pprof path", path: "/debug/pprof/x"},
		{name: "empty", path: "", wantErr: true},
		{name: "missing leading slash", path: "metrics", wantErr: true},
		{name: "leading space", path: " /metrics", wantErr: true},
		{name: "embedded space", path: "/met rics", wantErr: true},
		{name: "wildcard", path: "/m/{id}", wantErr: true},
		{name: "pprof conflict", path: "/debug/pprof/", enablePprof: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMetricsPath(tc.path, tc.enablePprof)
			if tc.wantErr && err == nil {
				t.Fatalf("want error for %q, got none", tc.path)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.path, err)
			}
			// A path validateMetricsPath accepts must not panic the mux.
			if err == nil {
				if _, err := buildMux(tc.path, http.NotFoundHandler(), tc.enablePprof); err != nil {
					t.Fatalf("buildMux rejected accepted path %q: %v", tc.path, err)
				}
			}
		})
	}
}

// FuzzBuildMux checks the property the table above samples: every path that
// validateMetricsPath accepts can be registered. http.ServeMux panics on a
// malformed or conflicting pattern, so a path that slipped through validation
// would crash the exporter at startup.
func FuzzBuildMux(f *testing.F) {
	for _, path := range []string{
		"/metrics", "/", "/a/b/metrics", "/metrics/", "/debug/pprof", "/debug/pprof/x",
		"", "metrics", " /metrics", "/m{id}", "/%zz", "/a/../b", "//", "/a?b=c", "/a#b",
	} {
		f.Add(path, false)
		f.Add(path, true)
	}
	f.Fuzz(func(t *testing.T, path string, enablePprof bool) {
		if validateMetricsPath(path, enablePprof) != nil {
			return
		}
		if _, err := buildMux(path, http.NotFoundHandler(), enablePprof); err != nil {
			t.Fatalf("buildMux rejected %q (pprof %t), which validateMetricsPath accepted: %v", path, enablePprof, err)
		}
	})
}

func TestParseFlags_Defaults(t *testing.T) {
	restoreTestFlags(t)
	// Empty environment variables count as unset, so every flag below keeps
	// its default.
	clearFlagEnv(t)

	f, err := parseTestFlags("--jellyfin.token", "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := flags{
		MetricsPath:        "/metrics",
		MaxRequests:        40,
		MaxProcs:           1,
		WebListenAddresses: []string{":9594"},
		LogLevel:           "info",
		LogFormat:          "logfmt",
	}
	if !reflect.DeepEqual(*f, want) {
		t.Fatalf("want defaults %+v, have %+v", want, *f)
	}

	address, token, err := config.JellyfinInfo(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if address != "http://localhost:8096" || token != "token" {
		t.Fatalf("want the default address and the flag's token, have %q and %q", address, token)
	}
}

func TestParseFlags_Environment(t *testing.T) {
	restoreTestFlags(t)
	t.Setenv("JELLYFIN_ADDRESS", "http://jellyfin.example:8096")
	t.Setenv("JELLYFIN_TOKEN", "env-token")
	t.Setenv("GOMAXPROCS", "4")

	f, err := parseTestFlags()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want, have := 4, f.MaxProcs; want != have {
		t.Fatalf("want --runtime.gomaxprocs %d from GOMAXPROCS, have %d", want, have)
	}

	address, token, err := config.JellyfinInfo(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if address != "http://jellyfin.example:8096" || token != "env-token" {
		t.Fatalf("want the address and token from the environment, have %q and %q", address, token)
	}
}

func TestParseFlags_RequiresToken(t *testing.T) {
	restoreTestFlags(t)
	// An empty JELLYFIN_TOKEN must not satisfy the required flag.
	clearFlagEnv(t)

	if _, err := parseTestFlags(); err == nil || !strings.Contains(err.Error(), "--jellyfin.token") {
		t.Fatalf("want a missing --jellyfin.token error, have %v", err)
	}
}

func TestParseFlags_ListenAddresses(t *testing.T) {
	restoreTestFlags(t)

	// The flag repeats for several addresses, and a comma is not a separator.
	f, err := parseTestFlags("--jellyfin.token", "token", "--web.listen-address", ":9100", "--web.listen-address=[::1]:9100,:9101")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{":9100", "[::1]:9100,:9101"}
	if have := *f.webFlags().WebListenAddresses; !slices.Equal(have, want) {
		t.Fatalf("want listen addresses %q, have %q", want, have)
	}
}

func TestParseFlags_CollectorToggles(t *testing.T) {
	restoreTestFlags(t)

	f, err := parseTestFlags("--jellyfin.token", "token", "--collector.disable-defaults", "--collector.transcoding", "--no-collector.users")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !f.DisableDefaultCollectors {
		t.Fatal("want --collector.disable-defaults set")
	}

	// run applies --collector.disable-defaults like this; the collectors named
	// on the command line keep the state they were given.
	collector.DisableDefaultCollectors()
	nc, err := collector.NewJellyfinCollector(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if have := slices.Sorted(maps.Keys(nc.Collectors)); !slices.Equal(have, []string{"transcoding"}) {
		t.Fatalf("want only transcoding enabled, have %q", have)
	}
}

func TestParseFlags_RejectsUnknownLogLevel(t *testing.T) {
	restoreTestFlags(t)

	if _, err := parseTestFlags("--jellyfin.token", "token", "--log.level", "verbose"); err == nil {
		t.Fatal("want an error for an unknown --log.level")
	}
}

func TestParseFlags_SystemdSocketIsLinuxOnly(t *testing.T) {
	restoreTestFlags(t)

	f, err := parseTestFlags("--jellyfin.token", "token", "--web.systemd-socket")
	if runtime.GOOS != "linux" {
		if err == nil {
			t.Fatal("want --web.systemd-socket rejected off Linux")
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !*f.webFlags().WebSystemdSocket {
		t.Fatal("want --web.systemd-socket set")
	}
}

func TestParseFlags_Version(t *testing.T) {
	restoreTestFlags(t)

	var out strings.Builder
	exitCode := -1
	var f flags
	parser := newParser(&f, kong.Writers(&out, io.Discard), kong.Exit(func(code int) { exitCode = code }))
	if err := parseFlags(parser, []string{"--version", "--jellyfin.token", "token"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("want exit code 0, have %d", exitCode)
	}
	if want, have := versionString("jellyfin_exporter")+"\n", out.String(); want != have {
		t.Fatalf("want version output %q, have %q", want, have)
	}
}

func TestParseFlags_HelpListsEveryFlag(t *testing.T) {
	restoreTestFlags(t)

	var out strings.Builder
	var f flags
	parser := newParser(&f, kong.Writers(&out, io.Discard), kong.Exit(func(int) {}))
	if err := parseFlags(parser, []string{"--help", "--jellyfin.token", "token"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"--web.telemetry-path", "--[no-]web.disable-exporter-metrics", "--web.max-requests",
		"--[no-]collector.disable-defaults", "--runtime.gomaxprocs", "--[no-]web.enable-pprof",
		"--web.listen-address", "--web.config.file", "--log.level", "--log.format", "--version",
		"--jellyfin.address", "--jellyfin.token", "--collector.activity.days",
	}
	for _, name := range []string{"activity", "media", "playing", "storage", "system", "tasks", "transcoding", "users"} {
		want = append(want, "--[no-]collector."+name)
	}
	if runtime.GOOS == "linux" {
		want = append(want, "--[no-]web.systemd-socket")
	}
	for _, flag := range want {
		if !strings.Contains(out.String(), flag) {
			t.Errorf("--help does not list %s", flag)
		}
	}
}

func TestFlags_LogConfig(t *testing.T) {
	f := flags{LogLevel: "debug", LogFormat: "json"}
	cfg, err := f.logConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Level.String() != "debug" || cfg.Format.String() != "json" {
		t.Fatalf("want level debug and format json, have %s and %s", cfg.Level, cfg.Format)
	}

	for _, bad := range []flags{
		{LogLevel: "verbose", LogFormat: "logfmt"},
		{LogLevel: "info", LogFormat: "xml"},
	} {
		if _, err := bad.logConfig(); err == nil {
			t.Errorf("want an error for log level %q and format %q", bad.LogLevel, bad.LogFormat)
		}
	}
}
