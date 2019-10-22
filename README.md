    $ go run -tags noceph main.go -f -c ./sample.conf

    # modprobe nbd
    # nbd-client -b 4096 -C 1 -N sia localhost /dev/nbd0
    # nbd-client -b 4096 -C 1 -t 86400 -N sia localhost /dev/nbd0
    # nbd-client -d /dev/nbd0

    # LANG=C fdisk -l /dev/nbd0
    # mkfs.ext4 /dev/nbd0
    # mkfs.fat /dev/nbd0
