# Architecture

The Jellyfin Exporter is a single Go binary that turns the Jellyfin HTTP API into
Prometheus metrics. It keeps no state between scrapes: every request to the
metrics endpoint queries Jellyfin afresh.

## Components

| Component           | Code                                                                                      | Responsibility                                                                                                                                                                                                                                                                         |
|---------------------|-------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Command line        | `jellyfin_exporter.go`                                                                    | Parses flags and environment variables with [kong](https://github.com/alecthomas/kong). The Jellyfin connection flags and the collector flags are defined by the packages that use them and embedded into one command line.                                                          |
| HTTP server         | `jellyfin_exporter.go`, [exporter-toolkit](https://github.com/prometheus/exporter-toolkit) | Serves the metrics endpoint (`--web.telemetry-path`, `/metrics` by default), a landing page and, when enabled, pprof. exporter-toolkit adds TLS and basic authentication from `--web.config.file`.                                                                                   |
| Metrics handler     | `handler` in `jellyfin_exporter.go`                                                       | Serves scrapes from a registry built at start-up. The `collect[]` and `exclude[]` query parameters build a narrower one for a single scrape, and `--web.max-requests` limits parallel scrapes. The exporter's own `go_*`, `process_*` and `promhttp_*` metrics come from a separate registry. |
| Collector framework | `collector/collector.go`                                                                  | Each collector registers itself at start-up and gets a `--[no-]collector.<name>` flag. On each scrape, `JellyfinCollector` runs the enabled collectors in parallel and reports how long each took and whether it succeeded.                                                         |
| Collectors          | `collector/*.go`                                                                          | One per area of Jellyfin. Each calls the endpoints listed below and turns the response into metrics.                                                                                                                                                                                 |
| Jellyfin API client | `collector/utils`                                                                         | Authenticated `GET` requests with a 5-second timeout and a 64 MiB response limit, plus helpers that decode JSON and list sessions.                                                                                                                                                   |
| Configuration       | `config`                                                                                  | Holds the Jellyfin address and API token, and normalises them for every request.                                                                                                                                                                                                     |

## Collectors and the endpoints they call

| Collector   | Default  | Jellyfin endpoints                                            |
|-------------|----------|---------------------------------------------------------------|
| media       | enabled  | `/Items/Counts`                                               |
| playing     | enabled  | `/Sessions`, playing sessions only                            |
| system      | enabled  | `/System/Ping`, `/System/Info`                                |
| users       | enabled  | `/Users`, `/Sessions`                                         |
| activity    | disabled | `/user_usage_stats/user_activity` (Playback Reporting plugin) |
| storage     | disabled | `/System/Info/Storage`                                        |
| tasks       | disabled | `/ScheduledTasks`                                             |
| transcoding | disabled | `/Sessions`                                                   |

## Request flow

1. Prometheus sends `GET /metrics`, optionally with `collect[]` or `exclude[]`.
2. exporter-toolkit applies TLS and basic authentication, if configured.
3. The handler uses the start-up registry, or builds a narrower one for
   `collect[]` or `exclude[]`, and `JellyfinCollector` runs the chosen collectors
   in parallel.
4. Each collector sends authenticated `GET` requests to Jellyfin, decodes the
   JSON into typed structures and emits metrics.
5. The registry gathers the metrics, adds each collector's duration and success,
   and writes the response.

## Key properties

* **Read-only:** the exporter only sends `GET` requests to Jellyfin.
* **Stateless:** apart from reusing collector instances, nothing is kept between
  scrapes.
* **Failure isolation:** a collector that fails reports
  `jellyfin_scrape_collector_success 0`, and the others still report.
