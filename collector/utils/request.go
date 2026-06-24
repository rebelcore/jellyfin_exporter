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
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/common/version"
)

var defaultHTTPClient = &http.Client{Timeout: 5 * time.Second}

// maxResponseBytes caps how much of a Jellyfin API response GetHTTP will read
// into memory, guarding against a misbehaving or hostile endpoint streaming an
// unbounded body and exhausting the exporter's memory. It is deliberately far
// larger than any legitimate response from the endpoints this exporter calls
// (which scale with the number of users or active sessions, never with media
// library size), so valid data is never truncated. If a response does exceed
// it, GetHTTP returns an error rather than a silently truncated body, so the
// scrape fails loudly instead of emitting metrics parsed from partial data.
// It is a var (not a const) only so tests can shrink it.
var maxResponseBytes int64 = 64 << 20 // 64 MiB

// maxErrorSnippetBytes bounds how much of a non-2xx response body is echoed back
// in the returned error, keeping diagnostics useful without flooding logs.
const maxErrorSnippetBytes = 1 << 10 // 1 KiB

// userAgent is the value sent in the User-Agent header. Including the build
// version (when it has been stamped in via ldflags) lets Jellyfin operators
// identify which exporter, and which release, is querying their server.
func userAgent() string {
	if v := version.Version; v != "" {
		return "jellyfin_exporter/" + v
	}
	return "jellyfin_exporter"
}

// GetHTTP performs an authenticated GET against the Jellyfin API and returns the
// raw response body. It sets the MediaBrowser token and User-Agent, caps the
// amount read (see maxResponseBytes), and converts any non-2xx status into an
// error carrying a bounded snippet of the body.
func GetHTTP(url, token string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "MediaBrowser Token="+token)
	req.Header.Set("User-Agent", userAgent())

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer func(body io.ReadCloser) {
		_ = body.Close()
	}(resp.Body)

	// Read one byte past the cap so a body sitting exactly at the limit is kept
	// in full, while anything larger is detected and rejected rather than
	// silently truncated.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxResponseBytes {
		return nil, fmt.Errorf("response body from %s exceeds %d byte limit", url, maxResponseBytes)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if len(msg) > maxErrorSnippetBytes {
			msg = msg[:maxErrorSnippetBytes] + "..."
		}
		return nil, fmt.Errorf("unexpected HTTP status %s: %s", resp.Status, msg)
	}

	return body, nil
}
