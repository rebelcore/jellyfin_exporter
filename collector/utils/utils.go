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

// Package utils holds the Jellyfin HTTP API client (GetHTTP), the session data
// model shared across collectors, and small conversion helpers used when
// turning API responses into Prometheus metric values.
package utils

import (
	"strings"
)

// BoolToFloat maps a boolean to the 1/0 gauge value Prometheus expects.
func BoolToFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

// SystemUpValueFromPing reports 1 when the /System/Ping body is the expected
// "Jellyfin Server" marker (raw or JSON-quoted), else 0. Jellyfin returns this
// endpoint as plain text rather than JSON, so it is matched as a string.
func SystemUpValueFromPing(body []byte) int {
	s := strings.TrimSpace(string(body))
	if s == "Jellyfin Server" || s == "\"Jellyfin Server\"" {
		return 1
	}
	return 0
}
