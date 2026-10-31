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
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// ConnectionFlags are the Jellyfin connection settings as kong flags, which the
// exporter embeds in its command line (see Flags). Exactly one of
// jellyfin.token and jellyfin.token-file is required; jellyfin.address
// defaults to the local server.
type ConnectionFlags struct {
	Address   string `name:"jellyfin.address" env:"JELLYFIN_ADDRESS" default:"http://localhost:8096" placeholder:"http://localhost:8096" help:"Address to use for connecting to Jellyfin"`
	Token     string `name:"jellyfin.token" env:"JELLYFIN_TOKEN" required:"" xor:"token" placeholder:"TOKEN" help:"API Token to use for connecting to Jellyfin"`
	TokenFile string `name:"jellyfin.token-file" env:"JELLYFIN_TOKEN_FILE" type:"existingfile" required:"" xor:"token" placeholder:"FILE" help:"File holding the API Token, read on every scrape; use instead of --jellyfin.token"`
}

// connection is filled in when the command line is parsed. jellyfinURL,
// jellyfinToken and jellyfinTokenFile point into it and are dereferenced on
// every scrape via JellyfinInfo.
var (
	connection        ConnectionFlags
	jellyfinURL       = &connection.Address
	jellyfinToken     = &connection.Token
	jellyfinTokenFile = &connection.TokenFile
)

// Flags returns the connection settings for the exporter to embed in its
// command line with kong.Embed; parsing fills them in place.
func Flags() *ConnectionFlags {
	return &connection
}

// JellyfinInfo returns the configured Jellyfin base URL and API token, or an
// error if either is blank. Collectors call it at the start of every Update so
// a missing token surfaces as a failed scrape instead of a malformed request.
//
// With --jellyfin.token-file the token is read from the file on every call, so
// a rotated secret takes effect on the next scrape, and an unreadable file
// fails the scrape.
//
// Both values are normalised here rather than at each call site: surrounding
// whitespace is trimmed (a token read from a file or an environment variable
// often carries a trailing newline, which would otherwise be rejected by
// net/http as an invalid header value on every request) and any trailing
// slashes are stripped from the address, so the "%s/Path" URLs the collectors
// build never contain an empty path segment.
func JellyfinInfo(logger *slog.Logger) (string, string, error) {
	token := *jellyfinToken
	if *jellyfinTokenFile != "" {
		content, err := os.ReadFile(*jellyfinTokenFile)
		if err != nil {
			return "", "", fmt.Errorf("reading jellyfin token file: %w", err)
		}
		token = string(content)
	}

	url := strings.TrimRight(strings.TrimSpace(*jellyfinURL), "/")
	token = strings.TrimSpace(token)

	logger.Debug("Jellyfin URL", "Value", url)
	logger.Debug("Jellyfin token configured", "configured", token != "")

	if token == "" {
		return "", "", errors.New("missing jellyfin token")
	}
	if url == "" {
		return "", "", errors.New("missing jellyfin address")
	}

	return url, token, nil
}
