FROM alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

ARG TARGETARCH
COPY .build/linux-$TARGETARCH/jellyfin_exporter /bin/jellyfin_exporter

EXPOSE      9594
USER        nobody
ENTRYPOINT  [ "/bin/jellyfin_exporter" ]
