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

//go:build !nousers

package collector

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

type UserPolicy struct {
	IsDisabled      bool     `json:"IsDisabled"`
	IsAdministrator bool     `json:"IsAdministrator"`
	EnabledFolders  []string `json:"EnabledFolders"`
}

type JellyfinUser struct {
	Name             string     `json:"Name"`
	Id               string     `json:"Id"`
	LastActivityDate string     `json:"LastActivityDate"`
	Policy           UserPolicy `json:"Policy"`
}

type JellyfinSessionUser struct {
	UserId             string `json:"UserId"`
	UserName           string `json:"UserName"`
	Client             string `json:"Client"`
	ApplicationVersion string `json:"ApplicationVersion"`
	DeviceName         string `json:"DeviceName"`
	RemoteEndPoint     string `json:"RemoteEndPoint"`
}

type Account struct {
	Username   string
	UserID     string
	Active     int
	Admin      int
	LastActive string
	Access     []string
}

type userCollector struct {
	userAccount *prometheus.Desc
	userActive  *prometheus.Desc
	logger      *slog.Logger
}

func init() {
	registerCollector("users", defaultEnabled, NewUsersCollector)
}

func NewUsersCollector(logger *slog.Logger) (Collector, error) {
	const subsystem = "user"
	userAccount := prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "account"),
		"Jellyfin user accounts.",
		[]string{"user_id", "username", "admin", "last_access"}, nil,
	)
	userActive := prometheus.NewDesc(
		prometheus.BuildFQName(namespace, subsystem, "active"),
		"Jellyfin current active users.",
		[]string{"user_id", "username", "client", "client_version", "device", "ip_address"}, nil,
	)
	return &userCollector{
		userAccount: userAccount,
		userActive:  userActive,
		logger:      logger,
	}, nil
}

func getUserAccount(jellyfinURL, jellyfinToken string) ([]Account, error) {
	jellyfinAPIURL := fmt.Sprintf("%s/Users", jellyfinURL)
	rawData := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)

	rawBody, err := utils.CoerceToJSONBytes(rawData)
	if err != nil {
		return nil, err
	}

	var users []JellyfinUser
	if err := json.Unmarshal(rawBody, &users); err != nil {
		return nil, fmt.Errorf("unexpected response from Jellyfin API: %w", err)
	}

	accounts := make([]Account, 0, len(users))
	for _, u := range users {
		userLastActive := ""
		if u.LastActivityDate != "" {
			t, err := time.Parse(time.RFC3339, u.LastActivityDate)
			if err == nil {
				userLastActive = strconv.FormatInt(t.Unix(), 10)
			}
		}
		userActive := 1
		if u.Policy.IsDisabled {
			userActive = 0
		}
		userAdmin := 0
		if u.Policy.IsAdministrator {
			userAdmin = 1
		}

		accounts = append(accounts, Account{
			Username:   u.Name,
			UserID:     u.Id,
			Active:     userActive,
			Admin:      userAdmin,
			LastActive: userLastActive,
			Access:     u.Policy.EnabledFolders,
		})
	}
	return accounts, nil
}

func getUserActive(jellyfinURL, jellyfinToken string) ([]JellyfinSessionUser, error) {
	jellyfinAPIURL := fmt.Sprintf("%s/Sessions", jellyfinURL)
	rawData := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)

	rawBody, err := utils.CoerceToJSONBytes(rawData)
	if err != nil {
		return nil, err
	}

	var sessions []JellyfinSessionUser
	if err := json.Unmarshal(rawBody, &sessions); err != nil {
		return nil, fmt.Errorf("unexpected response from Jellyfin API: %w", err)
	}
	return sessions, nil
}

func (c *userCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, err := config.JellyfinInfo(c.logger)
	if err != nil {
		c.logger.Error("Failed to get Jellyfin config", "error", err)
		return err
	}

	userAccounts, err := getUserAccount(jellyfinURL, jellyfinToken)
	if err != nil {
		c.logger.Error("Failed to get user accounts", "error", err)
	}

	userActive, err := getUserActive(jellyfinURL, jellyfinToken)
	if err != nil {
		c.logger.Error("Failed to get user sessions", "error", err)
	}

	for _, userMap := range userAccounts {
		c.logger.Debug("Jellyfin user account", "Value", userMap.Username)
		ch <- prometheus.MustNewConstMetric(c.userAccount,
			prometheus.GaugeValue,
			float64(userMap.Active),
			userMap.UserID,
			userMap.Username,
			strconv.Itoa(userMap.Admin),
			userMap.LastActive,
		)
	}

	for _, session := range userActive {
		c.logger.Debug("Jellyfin user account active", "Value", session.UserName)
		remoteEndPoint := session.RemoteEndPoint

		ch <- prometheus.MustNewConstMetric(c.userActive,
			prometheus.GaugeValue,
			1,
			session.UserId,
			session.UserName,
			session.Client,
			session.ApplicationVersion,
			session.DeviceName,
			remoteEndPoint,
		)
	}

	return nil
}
