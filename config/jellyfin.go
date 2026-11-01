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
// exporter embeds in its command line (see Flags). jellyfin.address defaults to
// the local server. The token comes from exactly one of jellyfin.token and
// jellyfin.token-file: kong rejects the two together, and CheckToken requires
// one of them.
type ConnectionFlags struct {
	Address   string `name:"jellyfin.address" env:"JELLYFIN_ADDRESS" default:"http://localhost:8096" placeholder:"http://localhost:8096" help:"Address to use for connecting to Jellyfin"`
	Token     string `name:"jellyfin.token" env:"JELLYFIN_TOKEN" xor:"token" placeholder:"TOKEN" help:"API Token to use for connecting to Jellyfin. Required unless --jellyfin.token-file is set"`
	TokenFile string `name:"jellyfin.token-file" env:"JELLYFIN_TOKEN_FILE" type:"existingfile" xor:"token" placeholder:"FILE" help:"File holding the API Token, read on every scrape. Use instead of --jellyfin.token"`
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
	token, err := readToken()
	if err != nil {
		return "", "", err
	}
	url := strings.TrimRight(strings.TrimSpace(*jellyfinURL), "/")

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

// CheckToken returns an error unless the parsed command line gives a token,
// for the exporter to call once its flags are parsed. It stands in for kong's
// required flag: marking jellyfin.token and jellyfin.token-file required would
// put both in the usage line, as if both were needed, when kong rejects them
// together.
//
// A token file is read here as well, so one the exporter can't read, or one
// that is empty, stops it at startup rather than failing every scrape.
func CheckToken() error {
	token, err := readToken()
	if err != nil {
		return err
	}
	switch {
	case token != "":
		return nil
	case *jellyfinTokenFile != "":
		return fmt.Errorf("jellyfin token file %s is empty", *jellyfinTokenFile)
	default:
		return errors.New("missing flags: --jellyfin.token=TOKEN or --jellyfin.token-file=FILE")
	}
}

// readToken returns the API token with surrounding whitespace trimmed: the
// jellyfin.token value or, with jellyfin.token-file, what the file holds now.
func readToken() (string, error) {
	if *jellyfinTokenFile == "" {
		return strings.TrimSpace(*jellyfinToken), nil
	}
	content, err := os.ReadFile(*jellyfinTokenFile)
	if err != nil {
		return "", fmt.Errorf("reading jellyfin token file: %w", err)
	}
	return strings.TrimSpace(string(content)), nil
}
