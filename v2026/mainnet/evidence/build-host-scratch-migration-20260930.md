# Build-host scratch migration — 2026-09-30

The local build host had only 4.1 GiB available on `/` (reported as 100% used), while `/mnt/data` had approximately 732 GiB available. This was a release-build capacity hazard, not evidence about production storage sizing.

Two inactive September `sn-*` scratch trees were copied to `/mnt/data/sn-testnet/temp` with `rsync -aHAX --numeric-ids` at idle I/O priority. A metadata dry run with `--delete --itemize-changes` produced no differences for either tree. Each original path was then replaced by a symlink to the copied tree; the original copy was removed only after the symlink resolved and the target was readable.

| Original path (now a symlink) | Copied file size reported by rsync |
| --- | ---: |
| `/home/by/urnetwork/temp/sn-gate-rerun-v11-aHOWKRHL` | 28,888,985,897 bytes |
| `/home/by/urnetwork/temp/sn-integration-xOgvEe` | 26,790,396,978 bytes |

After both moves, `df -h` reported approximately 54 GiB available on `/` and 677 GiB on `/mnt/data`. Both original paths remained readable and neither migration backup remained. New mainnet qualification captures should use the explicit `/mnt/data` `TMPDIR` and `GOCACHE` settings in [MAINNET.md](../MAINNET.md); these moves do not qualify production disk capacity, backup restoration, retention, or full-volume behavior.
