<div align="center">

# Jellyfin Exporter

[![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/rebelcore/jellyfin_exporter/test.yml?style=for-the-badge&color=22C55E)](https://github.com/rebelcore/jellyfin_exporter/actions/workflows/test.yml)
[![GitHub Release](https://img.shields.io/github/v/release/rebelcore/jellyfin_exporter?style=for-the-badge&color=22C55E)](https://github.com/rebelcore/jellyfin_exporter/releases/latest)
[![Docker Pulls](https://img.shields.io/docker/pulls/rebelcore/jellyfin-exporter?style=for-the-badge&color=1D63ED)](https://hub.docker.com/r/rebelcore/jellyfin-exporter)
[![Docker Image Size](https://img.shields.io/docker/image-size/rebelcore/jellyfin-exporter/latest?style=for-the-badge&color=1D63ED)](https://hub.docker.com/r/rebelcore/jellyfin-exporter)
[![Go Report Card](https://img.shields.io/badge/Go_Report-A%2B-00ADD8?style=for-the-badge)](https://goreportcard.com/report/github.com/rebelcore/jellyfin_exporter)
[![License](https://img.shields.io/badge/license-Apache%202-g?style=for-the-badge&color=8B5CF6)](LICENSE)

Prometheus exporter for Jellyfin Media System metrics exposed
in Go with pluggable metric collectors.

</div>

---

## Installation and Usage

If you are new to Prometheus and `jellyfin_exporter` there is
a [simple step-by-step guide](https://docs.rebelcore.org/guides/jellyfin/exporter).

The `jellyfin_exporter` listens on HTTP port 9594 by default.
See the `--help` output for more options.

The flag `--jellyfin.token` is required. You can generate an API
Key in the Jellyfin admin dashboard.

If you want to use ENV based flags you can use the following.

```dotenv
JELLYFIN_ADDRESS=http://localhost:8096
JELLYFIN_TOKEN=TOKEN
```

### Ansible

Coming Soon!

### Docker

The `jellyfin_exporter` is designed to monitor your Jellyfin Media System.

For situations where containerized deployment is needed, you will
need to set the Jellyfin URL flag to use the docker container hostname.

```bash
docker run -d \
  -p 9594:9594 \
  rebelcore/jellyfin-exporter:latest \
  --jellyfin.address=http://jellyfin:8096 \
  --jellyfin.token=TOKEN
```

For Docker compose, similar flag changes are needed.

```yaml
---
services:
  jellyfin_exporter:
    image: rebelcore/jellyfin-exporter:latest
    container_name: jellyfin_exporter
    command:
      - '--jellyfin.address=http://jellyfin:8096'
      - '--jellyfin.token=TOKEN'
    ports:
      - 9594:9594
    restart: unless-stopped
```

## Grafana Dashboard
Refer to [this repository](https://github.com/rebelcore/jellyfin_grafana) to check out the official dashboard for the exporter.

## Collectors

There is varying support for collectors.
The tables below list all existing collectors.

Collectors are enabled by providing a `--collector.NAME` flag.
Collectors that are enabled by default can be disabled
by providing a `--no-collector.NAME` flag.
To enable only some specific collector(s),
use `--collector.disable-defaults --collector.NAME ...`.
For example, `--collector.media` enables the `media` collector.

### Enabled by default

| Name    | Description                                                                                            |
|---------|--------------------------------------------------------------------------------------------------------|
| media   | Exposes media totals in the system by type (`jellyfin_media_count`).                                   |
| playing | Exposes now playing state, progress, position, duration, and remaining time per session.               |
| system  | Exposes server online status (`jellyfin_up`), server info, and pending restart state.                  |
| users   | Exposes user accounts (active/disabled, admin status, last access) and active sessions with device info. |

### Disabled by default

`jellyfin_exporter` also implements a number of collectors that
are disabled by default. Reasons for this vary by collector,
and may include:

* Plugin Required

You can enable additional collectors as desired by adding them
to your init system's or service supervisor's startup configuration
for `jellyfin_exporter` but caution is advised. Enable at most one
at a time, testing first on a non-production system, then by hand
on a single production node. When enabling additional collectors,
you should carefully monitor the change by observing the
`scrape_duration_seconds` metric to ensure that collection completes
and does not time out. In addition, monitor the
`scrape_samples_post_metric_relabeling` metric to see the changes
in cardinality.

| Name        | Description                                                                                   |
|-------------|-----------------------------------------------------------------------------------------------|
| activity    | Exposes information from the Playback Reporting plugin.                                       |
| storage     | Exposes Jellyfin storage free/used bytes per location (program data, cache, libraries, etc.). |
| tasks       | Exposes scheduled task state, progress, last run duration, and last run status.                |
| transcoding | Exposes active transcoding session details (codecs, bitrate, framerate, resolution, etc.).     |

### Activity Collector

The `activity` collector can be enabled with `--collector.activity`.
It supports exposing metrics from the Playback Reporting plugin.
To use this collector you will need to enable the plugin first and
edit the setting `Keep data for` and set it to `Forever`. The option
`collector.activity.days` is set to 100 years in days by default to
show the max amount of data. You can modify the amount of days to pull
from, but it's recommended to leave it at its default for best data reporting.

### Transcoding Collector

The `transcoding` collector can be enabled with `--collector.transcoding`.
It exposes detailed metrics about active transcoding sessions including
codec information, bitrate, framerate, resolution, hardware acceleration
status, and transcoding reasons. Sessions are identified by a `session_id`
label so you can track individual streams.

### Metrics Reference

<details>
<summary>Expand for a full list of exported metrics</summary>

#### System

| Metric                               | Type  | Labels                                                      |
|--------------------------------------|-------|-------------------------------------------------------------|
| `jellyfin_up`                        | Gauge | —                                                           |
| `jellyfin_system_info`               | Gauge | server_id, server_name, version, product_name, package_name |
| `jellyfin_system_pending_restart`    | Gauge | —                                                           |

#### Media

| Metric               | Type  | Labels |
|----------------------|-------|--------|
| `jellyfin_media_count` | Gauge | type   |

#### Playing

| Metric                            | Type  | Labels                                                                                                  |
|-----------------------------------|-------|---------------------------------------------------------------------------------------------------------|
| `jellyfin_now_playing_state`      | Gauge | user_id, username, device, type, title, series_title, series_season, series_episode, method             |
| `jellyfin_now_playing_progress`   | Gauge | user_id, username, device, type, title, series_title, series_season, series_episode, method             |
| `jellyfin_now_playing_position`   | Gauge | user_id, username, device, type, title, series_title, series_season, series_episode, method             |
| `jellyfin_now_playing_duration`   | Gauge | user_id, username, device, type, title, series_title, series_season, series_episode, method             |
| `jellyfin_now_playing_remaining`  | Gauge | user_id, username, device, type, title, series_title, series_season, series_episode, method             |

#### Users

| Metric                  | Type  | Labels                                                       |
|------------------------|-------|--------------------------------------------------------------|
| `jellyfin_user_account` | Gauge | user_id, username, admin, last_access                        |
| `jellyfin_user_active`  | Gauge | user_id, username, client, client_version, device, ip_address |

#### Activity

| Metric                    | Type  | Labels                                        |
|--------------------------|-------|-----------------------------------------------|
| `jellyfin_activity_count` | Gauge | user_id, username, last_seen, total_play_time |

#### Storage

| Metric                       | Type  | Labels         |
|------------------------------|-------|----------------|
| `jellyfin_storage_free_bytes` | Gauge | location, name |
| `jellyfin_storage_used_bytes` | Gauge | location, name |

#### Tasks

| Metric                            | Type  | Labels                     |
|-----------------------------------|-------|----------------------------|
| `jellyfin_tasks_state`            | Gauge | task_name, category, state |
| `jellyfin_tasks_progress`         | Gauge | task_name, category        |
| `jellyfin_tasks_last_run_seconds` | Gauge | task_name, category        |
| `jellyfin_tasks_last_run_status`  | Gauge | task_name, category, status |

#### Transcoding

| Metric                                                    | Type  | Labels                                                                                                                                                                                         |
|-----------------------------------------------------------|-------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `jellyfin_transcoding_active`                             | Gauge | —                                                                                                                                                                                              |
| `jellyfin_transcoding_sessions`                           | Gauge | —                                                                                                                                                                                              |
| `jellyfin_transcoding_session_info`                       | Gauge | session_id, user_id, username, device, client, client_version, ip_address, media_type, title, series_title, series_season, series_episode, container, video_codec, audio_codec, audio_channels, hardware_acceleration, method |
| `jellyfin_transcoding_session_paused`                     | Gauge | session_id                                                                                                                                                                                     |
| `jellyfin_transcoding_session_bitrate_bits_per_second`    | Gauge | session_id                                                                                                                                                                                     |
| `jellyfin_transcoding_session_framerate`                  | Gauge | session_id                                                                                                                                                                                     |
| `jellyfin_transcoding_session_completion_percentage`      | Gauge | session_id                                                                                                                                                                                     |
| `jellyfin_transcoding_session_width_pixels`               | Gauge | session_id                                                                                                                                                                                     |
| `jellyfin_transcoding_session_height_pixels`              | Gauge | session_id                                                                                                                                                                                     |
| `jellyfin_transcoding_session_video_direct`               | Gauge | session_id                                                                                                                                                                                     |
| `jellyfin_transcoding_session_audio_direct`               | Gauge | session_id                                                                                                                                                                                     |
| `jellyfin_transcoding_session_reason`                     | Gauge | session_id, reason                                                                                                                                                                             |

#### Exporter

| Metric                                        | Type  | Labels                                                   |
|-----------------------------------------------|-------|----------------------------------------------------------|
| `jellyfin_exporter_build_info`                | Gauge | branch, goarch, goos, goversion, revision, tags, version |
| `jellyfin_scrape_collector_duration_seconds`  | Gauge | collector                                                |
| `jellyfin_scrape_collector_success`           | Gauge | collector                                                |

</details>

### Filtering enabled collectors

The `jellyfin_exporter` will expose all metrics from enabled collectors
by default. This is the recommended way to collect metrics to avoid errors.

For advanced use the `jellyfin_exporter` can be passed an optional list
of collectors to filter metrics. The `collect[]` parameter may be used
multiple times. In Prometheus configuration you can use this syntax under
the [scrape config](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#scrape_config).

```
  params:
    collect[]:
      - system
      - media
```

This can be useful for having different Prometheus servers collect
specific metrics from nodes.

You can also exclude collectors with `exclude[]` (do not combine `collect[]` and
`exclude[]` in the same request).

```
  params:
    exclude[]:
      - playing
```

## Development building and running

Prerequisites:

* [Go compiler](https://golang.org/dl/)
* RHEL/CentOS: `glibc-static` package.

Building:

    git clone https://github.com/rebelcore/jellyfin_exporter.git
    cd jellyfin_exporter
    make build
    ./jellyfin_exporter FLAGS

To see all available configuration flags:

    ./jellyfin_exporter --help

## Running tests

    make test

To generate a coverage report:

    go test -count=1 ./... -race -covermode=atomic -coverpkg=./... -coverprofile=coverage.out
    go tool cover -func=coverage.out

## TLS endpoint

**EXPERIMENTAL**

The exporter supports TLS via a new web configuration file.

```console
./jellyfin_exporter --web.config.file=web-config.yml
```

See
the [exporter-toolkit web-configuration](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md)
for more details.

## Profiling

**Disabled by default**

The exporter can serve Go [pprof](https://pkg.go.dev/net/http/pprof) profiling
endpoints under `/debug/pprof/` to help debug CPU or memory issues. They are
disabled by default and enabled with the `--web.enable-pprof` flag.

```console
./jellyfin_exporter --web.enable-pprof
```

When enabled, links to the profiles are also shown on the exporter's landing
page. Avoid exposing these endpoints publicly, as profiles can reveal internal
runtime details.
