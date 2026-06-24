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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/common/version"
)

func TestGetHTTP_SetsHeadersAndReturnsBody(t *testing.T) {
	const token = "abc123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "MediaBrowser Token="+token, r.Header.Get("Authorization"); want != have {
			t.Fatalf("want Authorization header %q, have %q", want, have)
		}
		if want, have := "jellyfin_exporter", r.Header.Get("User-Agent"); want != have {
			t.Fatalf("want User-Agent header %q, have %q", want, have)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	body, err := GetHTTP(srv.URL, token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want, have := "ok", string(body); want != have {
		t.Fatalf("want body %q, have %q", want, have)
	}
}

func TestGetHTTP_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, " boom \n", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := GetHTTP(srv.URL, "token")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "unexpected HTTP status") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected response body in error: %v", err)
	}
}

func TestGetHTTP_InvalidURL(t *testing.T) {
	if _, err := GetHTTP(":", "token"); err == nil {
		t.Fatalf("expected error")
	}
}

type errorRoundTripper struct{}

func (errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dial failed")
}

func TestGetHTTP_DoError(t *testing.T) {
	prev := defaultHTTPClient
	t.Cleanup(func() { defaultHTTPClient = prev })

	defaultHTTPClient = &http.Client{Transport: errorRoundTripper{}}
	if _, err := GetHTTP("http://example.invalid", "token"); err == nil {
		t.Fatalf("expected error")
	}
}

type errorBody struct{}

func (errorBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (errorBody) Close() error             { return nil }

type responseTransport struct{}

func (responseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(errorBody{}),
		Header:     make(http.Header),
	}, nil
}

func TestGetHTTP_ReadBodyError(t *testing.T) {
	prev := defaultHTTPClient
	t.Cleanup(func() { defaultHTTPClient = prev })

	defaultHTTPClient = &http.Client{Transport: responseTransport{}}
	if _, err := GetHTTP("http://example.invalid", "token"); err == nil {
		t.Fatalf("expected error")
	}
}

func TestGetHTTP_BodyAtLimitNotTruncated(t *testing.T) {
	prev := maxResponseBytes
	t.Cleanup(func() { maxResponseBytes = prev })
	maxResponseBytes = 32

	want := strings.Repeat("x", int(maxResponseBytes))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(want))
	}))
	defer srv.Close()

	body, err := GetHTTP(srv.URL, "token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != want {
		t.Fatalf("body at the limit must be returned in full: want %d bytes, have %d", len(want), len(body))
	}
}

func TestGetHTTP_BodyExceedsLimitErrors(t *testing.T) {
	prev := maxResponseBytes
	t.Cleanup(func() { maxResponseBytes = prev })
	maxResponseBytes = 32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", int(maxResponseBytes)+1)))
	}))
	defer srv.Close()

	body, err := GetHTTP(srv.URL, "token")
	if err == nil {
		t.Fatalf("expected error for over-limit body, got %d bytes", len(body))
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size-limit error, got: %v", err)
	}
	if body != nil {
		t.Fatalf("expected no body on over-limit error, got %d bytes", len(body))
	}
}

func TestGetHTTP_UserAgentIncludesVersion(t *testing.T) {
	prev := version.Version
	t.Cleanup(func() { version.Version = prev })
	version.Version = "9.9.9"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want, have := "jellyfin_exporter/9.9.9", r.Header.Get("User-Agent"); want != have {
			t.Errorf("want User-Agent %q, have %q", want, have)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	if _, err := GetHTTP(srv.URL, "token"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetHTTP_Non2xxBodyTruncatedInError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("z", maxErrorSnippetBytes*2)))
	}))
	defer srv.Close()

	_, err := GetHTTP(srv.URL, "token")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "...") {
		t.Fatalf("expected truncated-body marker in error, got: %v", err)
	}
	// The oversized response body must not be echoed back in full.
	if len(err.Error()) > maxErrorSnippetBytes+128 {
		t.Fatalf("error message not truncated: %d bytes", len(err.Error()))
	}
}
