FROM scratch

ARG TARGETARCH
COPY .build/linux-$TARGETARCH/jellyfin_exporter /bin/jellyfin_exporter

EXPOSE      9594
USER        65534:65534
ENTRYPOINT  [ "/bin/jellyfin_exporter" ]
