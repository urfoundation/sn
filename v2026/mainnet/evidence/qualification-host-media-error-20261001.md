# Qualification host storage incident and mainnet lesson

On 2026-10-01, old test scratch was moved from `goofe`'s root filesystem
(`/dev/sda`, an Apple SSD) to `/mnt/data` (`/dev/sdc1`). Nine inactive
workspace directories and 148 old `/tmp` directories were relocated with
their original paths preserved as symlinks. Each completed workspace copy
passed an `rsync` inventory comparison before its root copy was removed.
Root free space rose from about 11 GiB to 184 GiB. The relocation inventory
is `/mnt/data/urnetwork-temp/relocation-20261001.json`.

The tenth workspace did **not** pass. `rsync` exited 23 while reading an old
`sim-latency` binary from
`/home/by/urnetwork/temp/sn-server-tidy-identity-v1-gAh1RC`. The kernel
reported `Unrecovered read error - auto reallocate failed` and `I/O error,
dev sda, sector 1768804608` at 22:40 UTC. The same kernel log also records
unrecovered reads at sector 1683892544 on September 29 and 30. The original
tenth workspace remains at its root path. Its partial destination copy is
marked `_RELOCATION_INCOMPLETE.txt` and must not be treated as complete.
No SMART diagnosis or full-device scan was performed, and this is **not** an
observation of Snow or a mainnet service host.

For mainnet, free-byte and inode alerts alone are insufficient. Monitor
device/kernel media errors and backup integrity independently of service
health. Put release scratch and durable evidence on reviewed volumes; verify
every migration before switching a path, retain an incomplete source when a
copy fails, and prove restore from the actual production backup. A healthy
application process cannot certify a storage device or an unverified copy.
