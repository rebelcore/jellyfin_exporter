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

import "testing"

func TestBoolToFloat(t *testing.T) {
	if BoolToFloat(true) != 1 {
		t.Fatalf("expected true=1")
	}
	if BoolToFloat(false) != 0 {
		t.Fatalf("expected false=0")
	}
}

func TestSystemUpValueFromPing(t *testing.T) {
	tests := []struct {
		body []byte
		want int
	}{
		{body: []byte("Jellyfin Server"), want: 1},
		{body: []byte("\"Jellyfin Server\""), want: 1},
		{body: []byte("  Jellyfin Server \n"), want: 1},
		{body: []byte("nope"), want: 0},
		{body: []byte(""), want: 0},
	}
	for _, tt := range tests {
		if got := SystemUpValueFromPing(tt.body); got != tt.want {
			t.Fatalf("want %d, got %d (body=%q)", tt.want, got, string(tt.body))
		}
	}
}
