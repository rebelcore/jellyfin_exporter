# Contributing

Rebel Media uses GitHub to manage reviews of pull requests.

* If you have a trivial fix or improvement, go ahead and create a pull request,
  addressing (with `@...`) the maintainer of this repository (see
  [MAINTAINERS.md](MAINTAINERS.md)) in the description of the pull request.

* Relevant coding style guidelines are the [Go Code Review
  Comments](https://code.google.com/p/go-wiki/wiki/CodeReviewComments)
  and the _Formatting and style_ section of Peter Bourgon's [Go: Best
  Practices for Production
  Environments](http://peter.bourgon.org/go-in-production/#formatting-and-style).

* Sign your work to certify that your changes were created by yourself, or you
  have the right to submit it under our license. Read
  https://developercertificate.org/ for all details and append your sign-off to
  every commit message like this:

        Signed-off-by: Random J Developer <example@example.com>

## Collector Implementation Guidelines

The Jellyfin Exporter is not a general monitoring agent. Its sole purpose is to
expose metrics, as opposed to service metrics.

The Jellyfin Exporter tries to support the most common metrics.
