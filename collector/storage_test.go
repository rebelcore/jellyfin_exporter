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

//go:build !nostorage

package collector

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestGetSystemStorage(t *testing.T) {
	const token = "abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "/System/Info/Storage", r.URL.Path; want != have {
			t.Fatalf("want path %q, have %q", want, have)
		}
		if want, have := "MediaBrowser Token="+token, r.Header.Get("Authorization"); want != have {
			t.Fatalf("want Authorization header %q, have %q", want, have)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ProgramDataFolder":{"FreeSpace":100,"UsedSpace":50},
			"TranscodingTempFolder":{"FreeSpace":200,"UsedSpace":25},
			"Libraries":[
				{"Id":"lib-1","Name":"Movies","Folders":[{"FreeSpace":300,"UsedSpace":10}]}
			]
		}`))
	}))
	defer srv.Close()

	storage, err := getSystemStorage(srv.URL, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if storage.ProgramDataFolder == nil || storage.ProgramDataFolder.FreeSpace != 100 {
		t.Fatalf("unexpected ProgramDataFolder: %#v", storage.ProgramDataFolder)
	}
	if len(storage.Libraries) != 1 || storage.Libraries[0].Name != "Movies" {
		t.Fatalf("unexpected Libraries: %#v", storage.Libraries)
	}
}

func TestGetSystemStorage_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer srv.Close()

	_, err := getSystemStorage(srv.URL, "token")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestStorageCollectorUpdate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := NewStorageCollector(logger)
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
	if metrics != 6 {
		t.Fatalf("expected 6 metrics, got %d", metrics)
	}
}
