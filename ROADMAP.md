# Roadmap

Where the Jellyfin Exporter is heading over the next year, through October
2027. Plans can change; to suggest something,
[open an issue](https://github.com/rebelcore/jellyfin_exporter/issues).

## Planned

* **Keep up with Jellyfin.** Follow API changes in new Jellyfin server releases
  and in the Playback Reporting plugin.
* **Verifiable releases.** Sign release artifacts and publish build provenance,
  so downloads and container images can be verified.
* **Reproducible builds.** Make release binaries bit-for-bit reproducible from a
  tagged commit.
* **Dashboard in step.** Keep the
  [Grafana dashboard](https://github.com/rebelcore/jellyfin_grafana) in line with
  metric changes.
* **Ongoing upkeep.** Stay on current Go and dependency versions, and keep CI,
  linting, fuzzing and vulnerability scanning running on every change.

## Not planned

* Changing anything in Jellyfin: the exporter stays read-only.
* Outputs other than the Prometheus exposition format.
* Host or operating-system metrics, which
  [node_exporter](https://github.com/prometheus/node_exporter) already covers.
