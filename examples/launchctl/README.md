# MacOS LaunchDaemon

If you're installing through a package manager, you probably don't need to deal
with this file.

The `plist` file should be put in `/Library/LaunchDaemons/` (user defined daemons), and the binary installed at
`/usr/local/bin/jellyfin_exporter`.

Ex. install globally by

    sudo cp -n jellyfin_exporter /usr/local/bin/
    sudo cp -n examples/launchctl/io.prometheus.jellyfin_exporter.plist /Library/LaunchDaemons/

    # Store the Jellyfin API key in a file only the exporter's user, nobody, can read
    echo 'TOKEN' | sudo tee /usr/local/etc/jellyfin_exporter.token > /dev/null
    sudo chown nobody /usr/local/etc/jellyfin_exporter.token
    sudo chmod 400 /usr/local/etc/jellyfin_exporter.token

    # Configure by dropping CLI arguments in a file
    echo '--jellyfin.token-file=/usr/local/etc/jellyfin_exporter.token' | sudo tee /usr/local/etc/jellyfin_exporter.args

    sudo launchctl bootstrap system/ /Library/LaunchDaemons/io.prometheus.jellyfin_exporter.plist

    # Check it's running
    sudo launchctl list | grep jellyfin_exporter

    # See full process state
    sudo launchctl print system/io.prometheus.jellyfin_exporter

    # View logs
    sudo tail /tmp/jellyfin_exporter.log
