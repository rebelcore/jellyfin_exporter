# Changelog

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
