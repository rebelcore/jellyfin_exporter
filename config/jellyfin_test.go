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
