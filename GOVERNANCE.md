# Governance

## Decision making

The Jellyfin Exporter is a Rebel Media project with a single maintainer, who
makes the final decision on changes, releases and the project's direction.
Decisions are made in the open on GitHub:

* Bugs, ideas and proposals are discussed in
  [issues](https://github.com/rebelcore/jellyfin_exporter/issues).
* Code changes reach `develop` through pull requests that must pass CI and be
  approved by a code owner. The maintainer commits release preparation and
  documentation updates directly.
* Disagreements are settled by discussion on the issue or pull request. If that
  doesn't resolve them, the maintainer decides and explains the decision there.

## Roles and responsibilities

| Role               | Who                                                                                              | Responsibilities                                                                                                                                                                                                                                   |
|--------------------|--------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Maintainer         | [@DylanUnderwood](https://github.com/DylanUnderwood), the code owner in [CODEOWNERS](.github/CODEOWNERS) | Triage issues, review and merge pull requests, cut releases, respond to security reports under the [security policy](SECURITY.md), keep the documentation current, and enforce the [code of conduct](CODE_OF_CONDUCT.md). |
| Contributor        | Anyone                                                                                           | Open issues and pull requests as described in [CONTRIBUTING.md](CONTRIBUTING.md), with every commit signed off under the Developer Certificate of Origin.                                                                                       |
| Release automation | [@rebel-bot](https://github.com/rebel-bot)                                                       | Signs the release tag and publishes the release through GitHub Actions when a release is merged to `master`.                                                                                                                                      |

The maintainers can be reached at the address in [MAINTAINERS.md](MAINTAINERS.md).
