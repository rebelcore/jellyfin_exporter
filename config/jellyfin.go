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
	"log/slog"

	"github.com/alecthomas/kingpin/v2"
)

var (
	jellyfinURL   = kingpin.Flag("jellyfin.address", "Address to use for connecting to Jellyfin").PlaceHolder("http://localhost:8096").Default("http://localhost:8096").String()
	jellyfinToken = kingpin.Flag("jellyfin.token", "API Token to use for connecting to Jellyfin").Required().PlaceHolder("TOKEN").String()
)

func JellyfinInfo(logger *slog.Logger) (string, string, error) {
	logger.Debug("Jellyfin URL", "Value", *jellyfinURL)
	logger.Debug("Jellyfin Token", "Value", *jellyfinToken)

	return *jellyfinURL, *jellyfinToken, nil
}
