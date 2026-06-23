FROM alpine:3.24.1

ARG TARGETARCH
COPY .build/linux-$TARGETARCH/jellyfin_exporter /bin/jellyfin_exporter

EXPOSE      9594
USER        nobody
ENTRYPOINT  [ "/bin/jellyfin_exporter" ]
