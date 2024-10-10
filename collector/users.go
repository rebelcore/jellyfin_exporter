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
// +build !nousers

package collector

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rebelcore/jellyfin_exporter/collector/utils"
	"github.com/rebelcore/jellyfin_exporter/config"
)

type userCollector struct {
	userAccount *prometheus.Desc
	userActive  *prometheus.Desc
	logger      *slog.Logger
}

type Account struct {
	Username    string
	User_ID     string
	Active      int
	Admin       int
	Last_Active string
	Access      []string
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

func getUserAccount(jellyfinURL, jellyfinToken string) []Account {
	jellyfinAPIURL := fmt.Sprintf("%s/Users", jellyfinURL)
	rawData := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)
	data := rawData.([]interface{})

	userAccount := make([]Account, len(data))

	for index, item := range data {
		dataUserMap := item.(map[string]interface{})
		dataPolicyMap := dataUserMap["Policy"].(map[string]interface{})
		userLastActive := ""

		if dataUserMap["LastActivityDate"] != nil {
			t, err := time.Parse(time.RFC3339, dataUserMap["LastActivityDate"].(string))
			if err != nil {
				continue
			}
			userLastActive = strconv.FormatInt(t.Unix(), 10)
		}

		userActive := 1
		if dataPolicyMap["IsDisabled"] == true {
			userActive = 0
		}
		userAdmin := 0
		if dataPolicyMap["IsAdministrator"] == true {
			userAdmin = 1
		}

		userEnabledFolders := make([]string, len(dataPolicyMap["EnabledFolders"].([]interface{})))
		for i, item := range dataPolicyMap["EnabledFolders"].([]interface{}) {
			userEnabledFolders[i] = item.(string)
		}

		userAccount[index].Username = dataUserMap["Name"].(string)
		userAccount[index].User_ID = dataUserMap["Id"].(string)
		userAccount[index].Active = userActive
		userAccount[index].Admin = userAdmin
		userAccount[index].Last_Active = userLastActive
		userAccount[index].Access = userEnabledFolders
	}
	return userAccount
}

func getUserActive(jellyfinURL, jellyfinToken string) []interface{} {
	jellyfinAPIURL := fmt.Sprintf("%s/Sessions", jellyfinURL)
	rawData := utils.GetHTTP(jellyfinAPIURL, jellyfinToken)
	data := rawData.([]interface{})
	return data
}

func (c *userCollector) Update(ch chan<- prometheus.Metric) error {
	jellyfinURL, jellyfinToken, nil := config.JellyfinInfo(c.logger)

	userAccounts := getUserAccount(jellyfinURL, jellyfinToken)
	userActive := getUserActive(jellyfinURL, jellyfinToken)
	for user := range userAccounts {
		userMap := userAccounts[user]
		c.logger.Debug("Jellyfin user account", "Value", userMap.Username)
		ch <- prometheus.MustNewConstMetric(c.userAccount,
			prometheus.GaugeValue,
			float64(userMap.Active),
			userMap.User_ID,
			userMap.Username,
			strconv.Itoa(userMap.Admin),
			userMap.Last_Active,
		)
	}
	for _, item := range userActive {
		userMap := item.(map[string]interface{})
		c.logger.Debug("Jellyfin user account active", "Value", userMap["UserName"].(string))
		ch <- prometheus.MustNewConstMetric(c.userActive,
			prometheus.GaugeValue,
			1,
			userMap["UserId"].(string),
			userMap["UserName"].(string),
			userMap["Client"].(string),
			userMap["ApplicationVersion"].(string),
			userMap["DeviceName"].(string),
			userMap["RemoteEndPoint"].(string),
		)
	}

	return nil
}
