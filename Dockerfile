FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

ARG TARGETARCH
COPY .build/linux-$TARGETARCH/jellyfin_exporter /bin/jellyfin_exporter

EXPOSE      9594
USER        nobody
ENTRYPOINT  [ "/bin/jellyfin_exporter" ]
