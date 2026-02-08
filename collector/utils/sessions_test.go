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

package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Tests for the sessions API helpers in `sessions.go`.

func TestGetNowPlayingSessions_QueryParams(t *testing.T) {
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
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	sessions, err := GetNowPlayingSessions(srv.URL, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected empty list, got %d", len(sessions))
	}
}

func TestGetActiveSessions_DoesNotSetIsPlaying(t *testing.T) {
	const token = "abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("IsPlaying"); got != "" {
			t.Fatalf("did not expect IsPlaying query param, got %q", got)
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	if _, err := GetActiveSessions(srv.URL, token); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetActiveSessions_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer srv.Close()

	if _, err := GetActiveSessions(srv.URL, "token"); err == nil {
		t.Fatalf("expected error")
	}
}
