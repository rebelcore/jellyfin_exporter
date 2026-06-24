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

// Package config exposes the Jellyfin connection settings (server address and
// API token) as command-line flags / environment variables that every
// collector reads when talking to the Jellyfin API.
package config

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/alecthomas/kingpin/v2"
)

// jellyfinURL and jellyfinToken are bound to their flags/env vars during
// kingpin parsing. jellyfin.token is Required; jellyfin.address defaults to the
// local server. They are dereferenced on every scrape via JellyfinInfo.
var (
	jellyfinURL   = kingpin.Flag("jellyfin.address", "Address to use for connecting to Jellyfin").Envar("JELLYFIN_ADDRESS").PlaceHolder("http://localhost:8096").Default("http://localhost:8096").String()
	jellyfinToken = kingpin.Flag("jellyfin.token", "API Token to use for connecting to Jellyfin").Envar("JELLYFIN_TOKEN").PlaceHolder("TOKEN").Required().String()
)

// JellyfinInfo returns the configured Jellyfin base URL and API token, or an
// error if either is blank. Collectors call it at the start of every Update so
// a missing token surfaces as a failed scrape instead of a malformed request.
func JellyfinInfo(logger *slog.Logger) (string, string, error) {
	logger.Debug("Jellyfin URL", "Value", *jellyfinURL)
	logger.Debug("Jellyfin token configured", "configured", *jellyfinToken != "")

	if strings.TrimSpace(*jellyfinToken) == "" {
		return "", "", errors.New("missing jellyfin token")
	}
	if strings.TrimSpace(*jellyfinURL) == "" {
		return "", "", errors.New("missing jellyfin address")
	}

	return *jellyfinURL, *jellyfinToken, nil
}
