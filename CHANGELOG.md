# Changelog

## 1.5.3
* [CHANGE] Add a `session_id` label to `jellyfin_user_active` and the `jellyfin_now_playing_*` metrics
* [CHANGE] Add a `path` label to `jellyfin_storage_free_bytes` and `jellyfin_storage_used_bytes`
* [CHANGE] Replace kingpin with kong for command-line parsing; flags are unchanged, but `@file` arguments, `--help-long` and `--help-man` are no longer supported
* [BUG FIX] Report every session when several share the same labels, instead of dropping all but one with a "collected before" error #119
* [BUG FIX] Report every folder of a library that maps several folders, instead of only the first
* [BUG FIX] Emit `jellyfin_up` as 0 when the Jellyfin server is unreachable
* [BUG FIX] Stop counting remuxing sessions in `jellyfin_transcoding_active` and `jellyfin_transcoding_sessions`
* [BUG FIX] Trim whitespace from the API token and trailing slashes from `--jellyfin.address`
* [BUG FIX] Reject an invalid `--web.telemetry-path` at startup instead of panicking
* [BUG FIX] Return 400 for an `exclude[]` query that excludes every collector
* [BUG FIX] Reject a non-numeric `--collector.activity.days`
* [BUG FIX] Keep API error messages valid UTF-8 when truncating them
* [CLEANUP] Upgrade Go toolchain to 1.27.2
* [CLEANUP] Update dependencies (client_golang 1.25.0, common 0.72.1, exporter-toolkit 0.20.0)
* [CLEANUP] Update Alpine base image to 3.24.2
* [CLEANUP] Group Dependabot updates and add a 7-day release cooldown
* [CLEANUP] Fix the integration tests and tidy collector internals

## 1.5.2
* [ENHANCEMENT] Add `--web.enable-pprof` flag to expose pprof profiling endpoints (disabled by default)
* [BUG FIX] Fix a data race when a scrape request excludes every collector
* [BUG FIX] Only show profiling links on the landing page when pprof is enabled
* [ENHANCEMENT] Limit Jellyfin API response size to avoid unbounded memory use
* [ENHANCEMENT] Include the build version in the HTTP User-Agent
* [CLEANUP] Upgrade Go toolchain to 1.26.4
* [CLEANUP] Pin Alpine base image by digest
* [CLEANUP] Harden CI/GitHub workflows (timeouts, concurrency, vulnerability scanning)
* [CLEANUP] Add code documentation and refresh the README

## 1.5.1
* [CLEANUP] Upgrade Go toolchain to 1.25.10
* [CLEANUP] Pin Alpine base image to 3.23.4
* [CLEANUP] Improve CI/GitHub workflow security

## 1.5.0
* [FEATURE] Add `transcoding` collector (disabled by default)
* [FEATURE] Add `storage` collector (disabled by default)
* [FEATURE] Add `tasks` collector (disabled by default)
* [ENHANCEMENT] Add playback position, duration, remaining, and progress metrics to `playing` collector
* [ENHANCEMENT] Add server info and pending restart metrics to `system` collector
* [ENHANCEMENT] Add user account details (admin status, last access) and active session info to `users` collector
* [ENHANCEMENT] Improve error handling across collectors
* [ENHANCEMENT] Simplify session data sharing between collectors
* [CLEANUP] Major codebase refactor with consistent coding patterns
* [CLEANUP] Improved test coverage
* [CLEANUP] Upgrade Go toolchain and modules
* [CLEANUP] Improve CI/GitHub workflows

## 1.4.0
* [ENHANCEMENT] Add env based flags
* [CLEANUP] Go mods, docker alpine and GitHub automation upgrades

## 1.3.9
* [ENHANCEMENT] Add activeWithinSeconds query to session api calls

## 1.3.8
* [ENHANCEMENT] Improve all collectors to work better with the jellyfin api

## 1.3.7
* [ENHANCEMENT] Improve user collection
* [CLEANUP] Mods upgrade

## 1.3.6
* [ENHANCEMENT] Improve error handling

## 1.3.5
* [ENHANCEMENT] Expose the type of stream (direct or transcoded) #33
* [ENHANCEMENT] Playback state #34
* [CLEANUP] Mods & docker alpine upgrade

## 1.3.4
* [BUG FIX] Return nil when user ip now present
* [BUG FIX] Only include season and episode info if found

## 1.3.3

* [ENHANCEMENT] Add device name to now playing records
* [CLEANUP] Mods upgrade

## 1.3.2

* [ENHANCEMENT] Add kubernetes example
* [CLEANUP] Mods upgrade

## 1.3.1

* [ENHANCEMENT] Upgrade logging to use `slog`
* [CLEANUP] Improve GitHub workflows

## 1.3.0

* [FEATURE] Add activity collector

## 1.2.0

* [FEATURE] Add playing collector

## 1.1.0

* [FEATURE] Add users collector
* [CLEANUP] Clean collector code

## 1.0.0

Initial Release

* [FEATURE] Add system collector
* [FEATURE] Add media collector
