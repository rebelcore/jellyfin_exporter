# Systemd Unit

The unit files (`*.service` and `*.socket`) in this directory are to be put into `/etc/systemd/system`.
It needs a user named `jellyfin_exporter`, whose shell should be `/sbin/nologin` and should not have any special
privileges.
It needs a sysconfig file in `/etc/sysconfig/jellyfin_exporter`.
A sample file can be found in `sysconfig.jellyfin_exporter`.
