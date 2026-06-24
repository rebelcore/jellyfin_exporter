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

//go:build !notasks

package collector

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

// jellyfinTaskResult is a task's LastExecutionResult. The error fields are
// pointers so they remain nil when the task did not fail.
type jellyfinTaskResult struct {
	StartTimeUtc     string  `json:"StartTimeUtc"`
	EndTimeUtc       string  `json:"EndTimeUtc"`
	Status           string  `json:"Status"`
	ErrorMessage     *string `json:"ErrorMessage"`
	LongErrorMessage *string `json:"LongErrorMessage"`
}

// jellyfinTaskInfo is one entry from /ScheduledTasks. Most fields are pointers
// because the API omits them depending on the task and its run history.
type jellyfinTaskInfo struct {
	Name                      *string             `json:"Name"`
	State                     string              `json:"State"`
	CurrentProgressPercentage *float64            `json:"CurrentProgressPercentage"`
	Id                        *string             `json:"Id"`
	LastExecutionResult       *jellyfinTaskResult `json:"LastExecutionResult"`
	Category                  *string             `json:"Category"`
	IsHidden                  bool                `json:"IsHidden"`
	Key                       *string             `json:"Key"`
}

// tasksCollector reports scheduled-task health: current run state, progress
// percentage, and the duration and status of the last execution.
type tasksCollector struct {
	state           *prometheus.Desc
	progressPercent *prometheus.Desc
	lastRunDuration *prometheus.Desc
	lastRunStatus   *prometheus.Desc
	logger          *slog.Logger
}

func init() {
	registerCollector("tasks", defaultDisabled, NewTasksCollector)
}

// NewTasksCollector builds the tasks collector and its descriptors. The state
// and last_run_status metrics carry the textual value as an extra label, so the
// gauge (1 = Running / Completed) and the raw status are both queryable.
func NewTasksCollector(logger *slog.Logger) (Collector, error) {
	const subsystem = "tasks"

	labelNames := []string{"task_name", "category"}

	return &tasksCollector{
		state: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "state"),
			"Current state of the task (1 = Running).",
			append(labelNames, "state"),
			nil,
		),
		progressPercent: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "progress"),
			"Task progress percentage, if provided.",
			labelNames,
			nil,
		),
		lastRunDuration: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "last_run_seconds"),
			"Last task execution duration in seconds, if provided.",
			labelNames,
			nil,
		),
		lastRunStatus: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, "last_run_status"),
			"Last task execution status (1 = Completed).",
			append(labelNames, "status"),
			nil,
		),
		logger: logger,
	}, nil
}

// getScheduledTasks fetches and decodes /ScheduledTasks.
func getScheduledTasks(jellyfinURL, jellyfinToken string) ([]jellyfinTaskInfo, error) {
	jellyfinAPIURL := fmt.Sprintf("%s/ScheduledTasks", jellyfinURL)
	rawBody, err := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)
	if err != nil {
		return nil, err
	}

	var tasks []jellyfinTaskInfo
	if err := json.Unmarshal(rawBody, &tasks); err != nil {
		return nil, fmt.Errorf("unexpected response from Jellyfin API: %w", err)
	}
	return tasks, nil
}

// Update emits state, progress, last-run duration and last-run status for each
// scheduled task. Each metric is skipped when its underlying field is absent,
// and the last-run duration is emitted only when both timestamps parse and the
// end is after the start.
func (c *tasksCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, err := config.JellyfinInfo(c.logger)
	if err != nil {
		c.logger.Error("Failed to get Jellyfin config", "error", err)
		return err
	}

	tasks, err := getScheduledTasks(jellyfinURL, jellyfinToken)
	if err != nil {
		c.logger.Error("Failed to get scheduled tasks", "error", err)
		return err
	}

	for _, task := range tasks {
		taskName := ""
		if task.Name != nil {
			taskName = *task.Name
		}
		category := ""
		if task.Category != nil {
			category = *task.Category
		}

		labelValues := []string{taskName, category}

		// Current run state: 1 when Running, with the textual state kept as a
		// label so other states (Idle, Cancelling) stay queryable.
		if task.State != "" {
			state := 0.0
			if task.State == "Running" {
				state = 1.0
			}
			ch <- prometheus.MustNewConstMetric(c.state, prometheus.GaugeValue, state, append(labelValues, task.State)...)
		}

		if task.CurrentProgressPercentage != nil {
			ch <- prometheus.MustNewConstMetric(c.progressPercent, prometheus.GaugeValue, *task.CurrentProgressPercentage, labelValues...)
		}

		if task.LastExecutionResult != nil {
			res := task.LastExecutionResult

			// Last-run duration, emitted only when both timestamps parse and the
			// run actually advanced (end after start); this guards against
			// missing or malformed timestamps.
			if res.StartTimeUtc != "" && res.EndTimeUtc != "" {
				start, startErr := time.Parse(time.RFC3339Nano, res.StartTimeUtc)
				end, endErr := time.Parse(time.RFC3339Nano, res.EndTimeUtc)
				if startErr == nil && endErr == nil && end.After(start) {
					ch <- prometheus.MustNewConstMetric(c.lastRunDuration, prometheus.GaugeValue, end.Sub(start).Seconds(), labelValues...)
				}
			}

			// Last-run outcome: 1 when Completed, with the raw status as a label.
			if res.Status != "" {
				status := 0.0
				if res.Status == "Completed" {
					status = 1.0
				}
				ch <- prometheus.MustNewConstMetric(c.lastRunStatus, prometheus.GaugeValue, status, append(labelValues, res.Status)...)
			}
		}
	}

	return nil
}
