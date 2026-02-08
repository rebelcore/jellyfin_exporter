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

//go:build !nostorage

package collector

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

type jellyfinFolderStorage struct {
	FreeSpace int64 `json:"FreeSpace"`
	UsedSpace int64 `json:"UsedSpace"`
}

type jellyfinLibraryStorage struct {
	ID      string                  `json:"Id"`
	Name    string                  `json:"Name"`
	Folders []jellyfinFolderStorage `json:"Folders"`
}

type jellyfinSystemStorage struct {
	ProgramDataFolder      *jellyfinFolderStorage   `json:"ProgramDataFolder"`
	WebFolder              *jellyfinFolderStorage   `json:"WebFolder"`
	ImageCacheFolder       *jellyfinFolderStorage   `json:"ImageCacheFolder"`
	CacheFolder            *jellyfinFolderStorage   `json:"CacheFolder"`
	LogFolder              *jellyfinFolderStorage   `json:"LogFolder"`
	InternalMetadataFolder *jellyfinFolderStorage   `json:"InternalMetadataFolder"`
	TranscodingTempFolder  *jellyfinFolderStorage   `json:"TranscodingTempFolder"`
	Libraries              []jellyfinLibraryStorage `json:"Libraries"`
}

type storageCollector struct {
	freeBytes *prometheus.Desc
	usedBytes *prometheus.Desc
	logger    *slog.Logger
}

func init() {
	registerCollector("storage", defaultDisabled, NewStorageCollector)
}

func NewStorageCollector(logger *slog.Logger) (Collector, error) {
	const subsystem = "storage"

	labelNames := []string{"location", "name"}

	return &storageCollector{
		freeBytes: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "free_bytes"),
			"Reported free bytes on the underlying storage for a given Jellyfin folder or library folder.",
			labelNames,
			nil,
		),
		usedBytes: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "used_bytes"),
			"Reported used bytes on the underlying storage for a given Jellyfin folder or library folder.",
			labelNames,
			nil,
		),
		logger: logger,
	}, nil
}

func getSystemStorage(jellyfinURL, jellyfinToken string) (*jellyfinSystemStorage, error) {
	jellyfinAPIURL := fmt.Sprintf("%s/System/Info/Storage", jellyfinURL)
	rawBody, err := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)
	if err != nil {
		return nil, err
	}
	var storage jellyfinSystemStorage
	if err := json.Unmarshal(rawBody, &storage); err != nil {
		return nil, fmt.Errorf("unexpected response from Jellyfin API: %w", err)
	}
	return &storage, nil
}

func (c *storageCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, err := config.JellyfinInfo(c.logger)
	if err != nil {
		c.logger.Error("Failed to get Jellyfin config", "error", err)
		return err
	}

	storage, err := getSystemStorage(jellyfinURL, jellyfinToken)
	if err != nil {
		c.logger.Error("Failed to get system storage", "error", err)
		return err
	}

	c.emitFolder(ch, "program_data", storage.ProgramDataFolder, "Program Data")
	c.emitFolder(ch, "web", storage.WebFolder, "Web")
	c.emitFolder(ch, "image_cache", storage.ImageCacheFolder, "Image Cache")
	c.emitFolder(ch, "cache", storage.CacheFolder, "Cache")
	c.emitFolder(ch, "logs", storage.LogFolder, "Logs")
	c.emitFolder(ch, "metadata", storage.InternalMetadataFolder, "Metadata")
	c.emitFolder(ch, "transcodes", storage.TranscodingTempFolder, "Transcodes")

	for _, lib := range storage.Libraries {
		for _, folder := range lib.Folders {
			c.emitFolder(ch, "library", &folder, lib.Name)
		}
	}

	return nil
}

func (c *storageCollector) emitFolder(ch chan<- prometheus.Metric, location string, folder *jellyfinFolderStorage, libraryName string) {
	if folder == nil {
		return
	}
	labelValues := []string{location, libraryName}
	ch <- prometheus.MustNewConstMetric(c.freeBytes, prometheus.GaugeValue, float64(folder.FreeSpace), labelValues...)
	ch <- prometheus.MustNewConstMetric(c.usedBytes, prometheus.GaugeValue, float64(folder.UsedSpace), labelValues...)
}
