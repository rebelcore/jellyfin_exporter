# Security

This document describes what you can and can't expect from the exporter in terms
of security, and why we believe those expectations hold. To report a
vulnerability, see [SECURITY.md](../SECURITY.md).

## What you can expect

* **Read-only access to Jellyfin.** The exporter only sends `GET` requests to the
  Jellyfin API. It never changes Jellyfin's data or configuration.
* **The API token stays private.** It is sent only to the configured
  `--jellyfin.address`, in the `Authorization` header, never in a URL. It isn't
  logged (debug logs record only whether a token is set) and never appears in
  metrics or error messages.
* **Verified TLS to Jellyfin.** When `--jellyfin.address` uses `https://`, the
  server's certificate is verified, and there is no option to turn verification
  off.
* **A metrics endpoint you can protect.** TLS 1.2 or newer and HTTP basic
  authentication with bcrypt-hashed passwords can be enabled through the
  [web configuration file](https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md)
  (`--web.config.file`). They apply to every route, including pprof.
* **Robust handling of Jellyfin responses.** Responses are capped at 64 MiB,
  requests time out after 5 seconds, and JSON is decoded into typed structures.
  A malformed response fails the scrape with an error rather than crashing the
  exporter; fuzz tests check this.
* **Validated input.** Flags are type-checked and validated at start-up. The
  `collect[]` query parameter accepts only enabled collector names, and
  `exclude[]` ignores unknown names with a warning.
* **Least privilege.** The container image runs as the unprivileged `nobody`
  user, and the exporter warns when it runs as root.

## What you can't expect

* **The token is powerful.** A Jellyfin API key grants administrator access to
  the server. Keep it in a secret store, such as a Kubernetes Secret, and limit
  who can read the exporter's environment and configuration.
* **Metrics contain personal data.** They include usernames, device and client
  names, client IP addresses and what each person is watching. Anyone who can
  reach the metrics endpoint can read them, so protect it with the web
  configuration file or network controls.
* **Plain HTTP to Jellyfin isn't encrypted.** The default address,
  `http://localhost:8096`, suits a Jellyfin server on the same host. For a remote
  server use `https://`, or the token travels in clear text.
* **pprof exposes internals.** `--web.enable-pprof` is off by default; enable it
  only on trusted networks.

## Assurance case

### Threat model

The assets are the Jellyfin API token, the personal data in the metrics, and the
availability of the exporter and of Jellyfin. The threats considered are:

* a network client reading the metrics, or overloading the exporter with scrapes;
* an attacker between the exporter and Jellyfin reading or altering traffic;
* a compromised or misbehaving Jellyfin server returning oversized or malformed
  responses;
* a compromised dependency or a tampered release.

Out of scope: an attacker with access to the host or to the exporter's
configuration, who can read the token directly.

### Trust boundaries

1. **Scrapers to exporter.** Requests to the HTTP endpoint come from the network
   and are untrusted. TLS and basic authentication, when configured, are enforced
   at this boundary.
2. **Exporter to Jellyfin.** Responses from the Jellyfin API are untrusted input.
3. **Operator to exporter.** Flags, environment variables and the web
   configuration file are trusted: they come from whoever runs the exporter.
4. **Supply chain to build.** Go modules come through the Go module proxy and are
   verified against `go.sum`. Releases are built by GitHub Actions from signed
   tags; their checksums and container images are signed with Sigstore, and the
   binaries can be rebuilt bit-for-bit. See
   [Verifying releases](verifying-releases.md).

### Secure design principles

* **Least privilege:** read-only use of the API, and the container image runs as
  `nobody`.
* **Fail-safe defaults:** the token is required, pprof is off, TLS certificates
  are always verified, a response over the size limit fails the scrape instead
  of being truncated, and a failing collector reports
  `jellyfin_scrape_collector_success 0`.
* **Economy of mechanism:** no state, no database and no write paths.
  Cryptography comes from Go's standard library and `golang.org/x/crypto`,
  through exporter-toolkit.
* **Complete mediation:** when basic authentication is configured,
  exporter-toolkit checks it on every request.
* **Open design:** everything is open source and built on standard, reviewed
  libraries.

### Common weaknesses countered

| Weakness                     | Countermeasure                                                                                                                                                                  |
|------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Injection                    | API URLs are built from the configured address and fixed paths, with query strings from `url.Values`. A lint rule (`depguard`) bans `os/exec` outside tests, so the exporter can't run other programs. |
| Resource exhaustion          | A 64 MiB response limit, a 5-second request timeout, and `--web.max-requests` to cap parallel scrapes.                                                                        |
| Malformed input              | Typed JSON decoding, validated flags and query parameters, and fuzz tests for the collectors and the HTTP error path.                                                         |
| Information exposure         | The token is never logged or exported, and error messages quote at most 1 KiB of a response body.                                                                              |
| Memory safety and data races | Go is memory-safe, and CI runs the tests with the race detector.                                                                                                               |
| Vulnerable dependencies      | Dependabot updates, govulncheck and CodeQL in CI, and OSV-Scanner through OpenSSF Scorecard.                                                                                  |
| Insecure transport           | TLS certificate verification is always on, and the metrics endpoint supports TLS 1.2 or newer.                                                                                |
