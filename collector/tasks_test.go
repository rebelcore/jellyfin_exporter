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

//go:build !notasks

package collector

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestGetScheduledTasks(t *testing.T) {
	const token = "abc123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "/ScheduledTasks", r.URL.Path; want != have {
			t.Fatalf("want path %q, have %q", want, have)
		}
		if want, have := "MediaBrowser Token="+token, r.Header.Get("Authorization"); want != have {
			t.Fatalf("want Authorization header %q, have %q", want, have)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"Name":"Scan media library",
				"State":"Idle",
				"CurrentProgressPercentage":null,
				"Id":"task-1",
				"Category":"Library",
				"IsHidden":false,
				"Key":"Jellyfin.Server.Implementations.ScheduledTasks.Tasks.RefreshMediaLibraryTask",
				"LastExecutionResult":{
					"StartTimeUtc":"2025-01-01T00:00:00Z",
					"EndTimeUtc":"2025-01-01T00:01:30Z",
					"Status":"Completed",
					"ErrorMessage":null,
					"LongErrorMessage":null
				}
			}
		]`))
	}))
	defer srv.Close()

	tasks, err := getScheduledTasks(srv.URL, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(tasks))
	}
	if tasks[0].Name == nil || *tasks[0].Name != "Scan media library" {
		t.Fatalf("unexpected Name: %v", tasks[0].Name)
	}
	if tasks[0].LastExecutionResult == nil || tasks[0].LastExecutionResult.Status != "Completed" {
		t.Fatalf("unexpected LastExecutionResult: %#v", tasks[0].LastExecutionResult)
	}
}

func TestGetScheduledTasks_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer srv.Close()

	_, err := getScheduledTasks(srv.URL, "token")
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestTasksCollectorUpdate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c, err := NewTasksCollector(logger)
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

func TestTasksCollectorUpdate_EmptyState(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "/ScheduledTasks", r.URL.Path; want != have {
			t.Fatalf("want path %q, have %q", want, have)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"Name":"Task with empty state",
				"Category":"Other",
				"State":"",
				"CurrentProgressPercentage":null,
				"LastExecutionResult":{
					"StartTimeUtc":"2025-01-01T00:00:00Z",
					"EndTimeUtc":"2025-01-01T00:00:10Z",
					"Status":"Completed"
				}
			}
		]`))
	}))
	defer srv.Close()

	setJellyfinFlags(t, srv.URL, testJellyfinToken)
	t.Cleanup(func() { setJellyfinFlags(t, testJellyfinURL, testJellyfinToken) })

	c, err := NewTasksCollector(logger)
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
	if metrics != 2 {
		t.Fatalf("expected 2 metrics (last_run_seconds + last_run_status), got %d", metrics)
	}
}
