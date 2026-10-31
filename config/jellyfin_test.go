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

package config

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestJellyfinInfo_ReturnsConfiguredValues(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevURL := *jellyfinURL
	prevToken := *jellyfinToken
	defer func() {
		*jellyfinURL = prevURL
		*jellyfinToken = prevToken
	}()

	*jellyfinURL = "http://example"
	*jellyfinToken = "token"

	url, token, err := JellyfinInfo(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "http://example" {
		t.Fatalf("unexpected url: %q", url)
	}
	if token != "token" {
		t.Fatalf("unexpected token: %q", token)
	}
}

func TestJellyfinInfo_EmptyTokenErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevURL := *jellyfinURL
	prevToken := *jellyfinToken
	defer func() {
		*jellyfinURL = prevURL
		*jellyfinToken = prevToken
	}()

	*jellyfinURL = "http://example"
	*jellyfinToken = ""

	_, _, err := JellyfinInfo(logger)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestJellyfinInfo_EmptyAddressErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevURL := *jellyfinURL
	prevToken := *jellyfinToken
	defer func() {
		*jellyfinURL = prevURL
		*jellyfinToken = prevToken
	}()

	*jellyfinURL = ""
	*jellyfinToken = "token"

	_, _, err := JellyfinInfo(logger)
	if err == nil {
		t.Fatalf("expected error")
	}
}

// TestJellyfinInfo_NormalisesAddressAndToken pins the two normalisations that
// keep a natural misconfiguration from taking the whole exporter down: a
// trailing slash would make every request path a double slash, which ASP.NET
// Core routing 404s, and a token carrying a trailing newline (common when it
// comes from a file or an environment variable) is rejected outright by
// net/http as an invalid header value.
func TestJellyfinInfo_NormalisesAddressAndToken(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevURL, prevToken := *jellyfinURL, *jellyfinToken
	t.Cleanup(func() { *jellyfinURL, *jellyfinToken = prevURL, prevToken })

	*jellyfinURL = "  http://jellyfin:8096//  "
	*jellyfinToken = "  secret\n"

	url, token, err := JellyfinInfo(logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want, have := "http://jellyfin:8096", url; want != have {
		t.Fatalf("want url %q, have %q", want, have)
	}
	if want, have := "secret", token; want != have {
		t.Fatalf("want token %q, have %q", want, have)
	}
}

// TestJellyfinInfo_WhitespaceOnlyTokenErrors ensures a token that is only
// whitespace is treated as missing rather than sent as a blank credential.
func TestJellyfinInfo_WhitespaceOnlyTokenErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevURL, prevToken := *jellyfinURL, *jellyfinToken
	t.Cleanup(func() { *jellyfinURL, *jellyfinToken = prevURL, prevToken })

	*jellyfinURL = "http://jellyfin:8096"
	*jellyfinToken = "   \t\n"

	if _, _, err := JellyfinInfo(logger); err == nil {
		t.Fatalf("expected an error for a whitespace-only token")
	}
}

// TestJellyfinInfo_ReadsTokenFileOnEveryCall pins --jellyfin.token-file's
// contract: the token is read from the file on each call, so a rotated secret
// takes effect without a restart, and is trimmed like a flag value.
func TestJellyfinInfo_ReadsTokenFileOnEveryCall(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevURL, prevToken, prevTokenFile := *jellyfinURL, *jellyfinToken, *jellyfinTokenFile
	t.Cleanup(func() { *jellyfinURL, *jellyfinToken, *jellyfinTokenFile = prevURL, prevToken, prevTokenFile })

	path := filepath.Join(t.TempDir(), "token")
	*jellyfinURL = "http://jellyfin:8096"
	*jellyfinToken = ""
	*jellyfinTokenFile = path

	for _, want := range []string{"first", "second"} {
		if err := os.WriteFile(path, []byte(want+"\n"), 0o600); err != nil {
			t.Fatalf("writing the token file: %v", err)
		}
		_, have, err := JellyfinInfo(logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want != have {
			t.Fatalf("want token %q, have %q", want, have)
		}
	}
}

// TestJellyfinInfo_UnreadableTokenFileErrors ensures a token file that has
// gone missing fails the scrape instead of sending an empty credential.
func TestJellyfinInfo_UnreadableTokenFileErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	prevURL, prevToken, prevTokenFile := *jellyfinURL, *jellyfinToken, *jellyfinTokenFile
	t.Cleanup(func() { *jellyfinURL, *jellyfinToken, *jellyfinTokenFile = prevURL, prevToken, prevTokenFile })

	*jellyfinURL = "http://jellyfin:8096"
	*jellyfinToken = ""
	*jellyfinTokenFile = filepath.Join(t.TempDir(), "missing")

	if _, _, err := JellyfinInfo(logger); err == nil {
		t.Fatalf("expected an error for an unreadable token file")
	}
}
