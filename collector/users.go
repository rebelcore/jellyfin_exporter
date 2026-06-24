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
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

// UserPolicy is the subset of a user's Policy block the exporter reads.
type UserPolicy struct {
	IsDisabled      bool     `json:"IsDisabled"`
	IsAdministrator bool     `json:"IsAdministrator"`
	EnabledFolders  []string `json:"EnabledFolders"`
}

// JellyfinUser is one entry from the /Users response.
type JellyfinUser struct {
	Name             string     `json:"Name"`
	Id               string     `json:"Id"`
	LastActivityDate string     `json:"LastActivityDate"`
	Policy           UserPolicy `json:"Policy"`
}

// Account is the flattened, metric-ready view of a JellyfinUser. Active and
// Admin are 1/0 gauge values; LastActive is a Unix timestamp string, left empty
// when the user has never been active or the date could not be parsed.
type Account struct {
	Username   string
	UserID     string
	Active     int
	Admin      int
	LastActive string
	Access     []string
}

// userCollector reports the full user roster (jellyfin_user_account) and the
// users with a currently active session (jellyfin_user_active).
type userCollector struct {
	userAccount *prometheus.Desc
	userActive  *prometheus.Desc
	logger      *slog.Logger
}

func init() {
	registerCollector("users", defaultEnabled, NewUsersCollector)
}

// NewUsersCollector builds the users collector and its metric descriptors.
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

// getUserAccount fetches /Users and converts each user into an Account,
// parsing LastActivityDate into a Unix timestamp where present.
func getUserAccount(jellyfinURL, jellyfinToken string) ([]Account, error) {
	jellyfinAPIURL := fmt.Sprintf("%s/Users", jellyfinURL)
	rawBody, err := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)
	if err != nil {
		return nil, err
	}

	var users []JellyfinUser
	if err := json.Unmarshal(rawBody, &users); err != nil {
		return nil, fmt.Errorf("unexpected response from Jellyfin API: %w", err)
	}

	accounts := make([]Account, 0, len(users))
	for _, u := range users {
		// Convert the last-activity timestamp to a Unix epoch string, leaving it
		// empty when the user has never been active or the date can't be parsed.
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

// Update emits one jellyfin_user_account series per user and one
// jellyfin_user_active series per active session. The account and session
// lookups fail independently: a failure in one still reports the other, and the
// joined error is returned so the scrape is marked unsuccessful.
func (c *userCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, err := config.JellyfinInfo(c.logger)
	if err != nil {
		c.logger.Error("Failed to get Jellyfin config", "error", err)
		return err
	}

	// Fetch the account roster and the active sessions independently: a failure
	// in one still reports the other, and both errors are joined at the end so
	// the scrape is still marked unsuccessful.
	userAccounts, err := getUserAccount(jellyfinURL, jellyfinToken)
	errAccounts := err
	if err != nil {
		c.logger.Error("Failed to get user accounts", "error", err)
	}

	activeSessions, err := utils.GetActiveSessions(jellyfinURL, jellyfinToken)
	errActive := err
	if err != nil {
		c.logger.Error("Failed to get user sessions", "error", err)
	}

	if errAccounts == nil {
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
	}

	if errActive == nil {
		for _, session := range activeSessions {
			c.logger.Debug("Jellyfin user account active", "Value", session.UserName)
			ch <- prometheus.MustNewConstMetric(c.userActive,
				prometheus.GaugeValue,
				1,
				session.UserId,
				session.UserName,
				session.Client,
				session.ApplicationVersion,
				session.DeviceName,
				session.RemoteEndPoint,
			)
		}
	}

	return errors.Join(errAccounts, errActive)
}
