**Please note: This is unfinished software and not ready for general usage.
This is a pre-release made available to Nebulous Labs developers for testing
purposes.**

# sia-nbdserver

This project implements an [NBD](https://nbd.sourceforge.io) server (almost
[baseline](https://sourceforge.net/p/nbd/code/ci/master/tree/doc/proto.md#baseline))
to expose [Sia](https://sia.tech/) cloud storage in the form of a
Linux block device `/dev/nbd0` in combination with a local cache.

## Quick Start

You will need a running Sia node that has formed storage contracts and is ready
to store data.

    $ go get -u github.com/javgh/sia-nbdserver     # will - by default - install to ~/go/bin/
    $ sia-nbdserver

As root:

    # modprobe nbd
    # nbd-client -b 4096 -t 3600 -u /run/user/1000/sia-nbdserver /dev/nbd0

    # mkfs.xfs /dev/nbd0
    # mount -o sync /dev/nbd0 /mnt

## Usage

    $ sia-nbdserver -h
    NBD server backed by Sia storage + local cache.

    Usage:
      sia-nbdserver [flags]

    Flags:
      -H, --hard int                   hard limit for number of 64 MiB pages in the cache (default 192)
      -h, --help                       help for sia-nbdserver
      -i, --idle int                   seconds to wait before a cache page is marked idle and upload begins (default 120)
          --sia-daemon string          host and port of Sia daemon (default "localhost:9980")
          --sia-password-file string   path to Sia API password file (default "/home/jan/.sia/apipassword")
      -s, --size uint                  size of block device; should ideally be a multiple of 67108864 (2 ^ 26) (default 1099511627776)
      -S, --soft int                   soft limit for number of 64 MiB pages in the cache (default 176)
      -u, --unix string                unix domain socket (default "/run/user/1000/sia-nbdserver")

By default `sia-nbdserver` will export a block device with a size of 1 TiB. This
can be changed with the `--size` flag. The software divides this range up into a
number of 64 MiB pages. As Sia continues to push the minimum file size lower, it
will be possible to make the pages smaller, but for now this value is hardcoded.
Each page will be stored on Sia as a separate file under the directory `nbd`.

A page is only created once it has been accessed for the first time. The
directory `~/.local/share/sia-nbdserver/` serves as a local cache, where
recently accessed pages are kept to speed up read and write operations. The
maximum number of pages in this cache can be set with `--soft` and `--hard`.
The software will actively try to reduce the size of the cache once the soft
limit has been reached, but will still allow the cache to grow if necessary.
Once the hard limit is reached, it will block new operations until necessary
uploads have completed. Some time after the soft limit is exceeded, a "write
throttle" kicks in, which will artificially slow down write operations to allow
Sia to catch up. This is done in an attempt to avoid outright blocking write
operations, which is prone to trigger timeouts in the NBD client.

There is no specific lower bound for the cache size, but it should probably not
be smaller than 16 pages and the hard limit should be an additional 8 pages for
the write throttle mechanic to work correctly. For a short test run it can be
helpful to reduce the time before uploads are initiated and use a fairly small
cache:

    $ sia-nbdserver --idle 30 -S 16 -H 32

Before shutting down the server, it is important to first unmount any filesystem
that might use `/dev/nbd0` and then tell `nbd-client` to disconnect:

    # umount /mnt
    # nbd-client -d /dev/nbd0

The server can then be shutdown with `^C` or using a `kill` command. This will
trigger a "fast" shutdown, where any unsynced data will simply remain in the
cache directory to be uploaded when the server is started again in the future.
To instead perform a "thorough" shutdown, it is possible to send `SIGUSR1` to
the server (use `kill -USR1 <pid of server>`). This will cause the server to
wait for all uploads to finish before shutting down.

## Pitfalls

In theory any filesystem can be used on top of the block device. I first tried
`ext4`, but realized that `mkfs.ext4` will immediately cause a huge amount of
upload activity by writing to a lot of different pages. I have found `xfs` to be
a good choice, as `mkfs.xfs` only touches a few pages.

Linux will provide caching for the NBD block device, which is great for
performance. However, it can cause a problem in this setting where `nbd-client`
and `sia-nbdserver` are on the same machine. If the write cache fills up too
much, it can use up all available memory, which in turn prevents `siad`  from
making any progress. Now the write cache can not get smaller and the system is
stuck. To prevent this I pass `-o sync` to `xfs` to force it to directly flush
all writes to the block device. This unfortunately impacts performance
negatively, but seems to avoid the low memory situation. Another approach would
be to have `nbd-client` and `sia-nbdserver` on two separate machines. This would
require to first modify `sia-nbdserver` to support exporting over the network.

In my testing (in December 2019) Sia sometimes makes no progress on uploads or
downloads for several tens of minutes. This will then usually cause `nbd-client`
to timeout. As a workaround I set a very high timeout value like 1 hour (3600 seconds)
with `nbd-client -t 3600`.
