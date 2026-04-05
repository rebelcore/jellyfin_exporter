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

//go:build !nousers

package collector

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestGetUserAccount(t *testing.T) {
	const token = "abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "/Users", r.URL.Path; want != have {
			t.Fatalf("want path %q, have %q", want, have)
		}
		if want, have := "MediaBrowser Token="+token, r.Header.Get("Authorization"); want != have {
			t.Fatalf("want Authorization header %q, have %q", want, have)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"Name":"Alice","Id":"u1","LastActivityDate":"2025-01-01T00:00:00Z","Policy":{"IsDisabled":false,"IsAdministrator":true,"EnabledFolders":["f1"]}},
			{"Name":"Bob","Id":"u2","LastActivityDate":"","Policy":{"IsDisabled":true,"IsAdministrator":false,"EnabledFolders":[]}}
		]`))
	}))
	defer srv.Close()

	accounts, err := getUserAccount(srv.URL, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("want 2 accounts, got %d", len(accounts))
	}

	if want, have := "Alice", accounts[0].Username; want != have {
		t.Fatalf("want username %q, have %q", want, have)
	}
	if accounts[0].Active != 1 {
		t.Fatalf("want active=1, got %d", accounts[0].Active)
	}
	if accounts[0].Admin != 1 {
		t.Fatalf("want admin=1, got %d", accounts[0].Admin)
	}
	if accounts[0].LastActive == "" {
		t.Fatalf("want non-empty last_active")
	}

	if accounts[1].Active != 0 {
		t.Fatalf("want active=0, got %d", accounts[1].Active)
	}
	if accounts[1].Admin != 0 {
		t.Fatalf("want admin=0, got %d", accounts[1].Admin)
	}
}

func TestGetUserAccount_InvalidLastActivityDate(t *testing.T) {
	const token = "abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "/Users", r.URL.Path; want != have {
			t.Fatalf("want path %q, have %q", want, have)
		}
		if want, have := "MediaBrowser Token="+token, r.Header.Get("Authorization"); want != have {
			t.Fatalf("want Authorization header %q, have %q", want, have)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"Name":"Alice","Id":"u1","LastActivityDate":"not-a-time","Policy":{"IsDisabled":false,"IsAdministrator":true,"EnabledFolders":[]}}
		]`))
	}))
	defer srv.Close()

	accounts, err := getUserAccount(srv.URL, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("want 1 account, got %d", len(accounts))
	}
	if accounts[0].LastActive != "" {
		t.Fatalf("expected empty LastActive for invalid timestamp, got %q", accounts[0].LastActive)
	}
}

func TestGetUserAccount_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer srv.Close()

	_, err := getUserAccount(srv.URL, "token")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestGetUserAccount_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := getUserAccount(srv.URL, "token")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestUsersCollectorUpdate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := NewUsersCollector(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ch := make(chan prometheus.Metric, 20)
	if err := c.Update(ch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	close(ch)

	metrics := 0
	for range ch {
		metrics++
	}
	if metrics != 4 {
		t.Fatalf("expected 4 metrics, got %d", metrics)
	}
}
