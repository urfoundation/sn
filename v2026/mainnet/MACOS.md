# Running sn-mainnet on macOS

Prepared 2026-10-06. `sn-mainnet` builds and runs on macOS (darwin/arm64 and
darwin/amd64) as well as Linux (amd64/arm64). Every physical custody path used
by the launch handoff in [LAUNCH.md](LAUNCH.md) has a macOS implementation with
the same admission and crash contract as Linux: storage preparation and
restore, bootstrap journals, owner trim and signing custody, contract action
journals, successor and Safe stores, treasury commands and validator
attempt-ledger custody. Features that depend on Linux kernel facilities refuse
explicitly on macOS instead of substituting weaker behavior; they are listed
below.

This document records the platform contract. It grants no launch authority and
does not replace the independent approvals, pins and readbacks in LAUNCH.md.

## Toolchain and build

The pinned toolchain is Go 1.27.1 for both platforms; `go.mod` requires it.
Build the operator binary from the reviewed checkout with the sibling
repositories that `go.mod` replaces:

```sh
GOTOOLCHAIN=local CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=true \
  -o /absolute/path/sn-mainnet ./mainnet
go version -m /absolute/path/sn-mainnet
shasum -a 256 /absolute/path/sn-mainnet
```

Retain the build information and digest with the other launch locks. The
production release composition in [RELEASE-BUILD.md](RELEASE-BUILD.md) remains a
Linux build-host tool for linux/amd64 server images; it does not produce the
macOS operator binary.

## Custody volume

A custody declaration names its volume by mount point, filesystem uuid and
type. On macOS:

| Field | Value |
| --- | --- |
| `filesystem_type` | `apfs` |
| `filesystem_uuid` | The APFS volume uuid, lowercase: `diskutil info -plist MOUNT \| plutil -extract VolumeUUID raw - \| tr A-F a-f` |
| `mount_path` | The volume's exact mount point, for example `/Volumes/URCustody` |

Only a local APFS volume with ownership enforced that is not a snapshot mount
qualifies. Any other APFS mount is reported as `apfs-unqualified` and refused:
the sealed system snapshot at `/`, Time Machine snapshot mounts, network shares
and volumes mounted with ownership ignored. The uuid must resolve to exactly
one qualified mounted volume; an unmounted volume is unavailable, not lost.

Daemon custody (the host run directory, contract and successor journals) must
live on a dedicated volume. As on Linux, where it may not share the root
filesystem, it may not use `/` or any volume under `/System/Volumes`, including
the data volume that holds home directories. Owner-local device custody may use
the data volume through its physical path below `/System/Volumes/Data`.

Create a dedicated volume in the internal APFS container (find the container,
for example `disk3`, with `diskutil list internal`):

```sh
sudo diskutil apfs addVolume disk3 "Case-sensitive APFS" URCustody
sudo diskutil enableOwnership /Volumes/URCustody
sudo mdutil -i off /Volumes/URCustody
sudo mkdir -m 0700 /Volumes/URCustody/sn25
sudo chown "$(id -un)" /Volumes/URCustody/sn25
```

Case-sensitive APFS is recommended for a new custody volume. Custody names are
program-chosen ASCII and an exclusive create refuses a case-only collision, but
a case-sensitive volume removes the aliasing entirely. A dedicated volume
shares its container's free space; the declared byte and inode reserves are
still checked before every write.

Paths in requests and declarations must be physical. `/var`, `/tmp` and `/etc`
are symlinks to `/private/...` on macOS, and every custody walk refuses a
symlinked component; use the `/private/...` path. Each ancestor must be owned
by root or the custody user and must not be group- or world-writable unless it
is a root-owned sticky directory.

Do not browse custody directories in Finder or index them with Spotlight.
Finder writes `.DS_Store` files, which would change an inventory. macOS also
attaches its own `com.apple.*` extended attributes, such as
`com.apple.provenance`, to files; custody ignores attributes outside the
`user.urnetwork.` namespace, so those are harmless.

## Platform semantics

| Contract | Linux | macOS |
| --- | --- | --- |
| Durable sync | `fsync` | `F_FULLFSYNC` (Go `os.File.Sync`), files and directories |
| No-replace / exchange rename | `renameat2` | `renameatx_np` with `RENAME_EXCL` / `RENAME_SWAP` |
| Absent extended attribute | `ENODATA` | `ENOATTR` (macOS `ENODATA` is unrelated) |
| Attribute create/replace | `fsetxattr` flags | libc `fsetxattr` options |
| Mount census | `/proc/self/mountinfo` | `getfsstat` |
| Filesystem uuid | `/dev/disk/by-uuid` | `getattrlist(ATTR_VOL_UUID)` at the exact mount point |
| Device number | 64-bit `dev_t` | 32-bit `dev_t`, widened without sign extension |
| Write-health probe | anonymous `O_TMPFILE` inode in the root | random name beside the external lease, unlinked before any write |

The custody syscalls live in `github.com/urnetwork/connect/durablesys`. On macOS,
`golang.org/x/sys/unix.Fsetxattr` (through at least v0.48.0) passes zero options
and silently drops `XATTR_CREATE`/`XATTR_REPLACE`; custody code must use
`durablesys.SetAttribute`, never `unix.Fsetxattr`, and compare absence with
`durablesys.ErrNoAttribute`, never `ENODATA`.

The write-health probe needs the lease file's directory to be writable by the
custody user. Declaration validation keeps that directory outside every
journal root, so an interruption between create and unlink can leave at most an
empty `.urnetwork-write-health-*` name there, never inside a custody inventory.

## Linux-only features

These refuse on macOS before any action:

- Historical replay and proof capture, and the native fee verifier. They
  execute an approved, pinned Linux ELF engine from a sealed memfd under a
  subreaper supervisor; macOS has no sealed anonymous executables or
  subreaper, and the engines are Linux artifacts.
- Validator repair, which requires a unified cgroup v2 host, and operator
  repair, which reads POSIX access ACLs as Linux extended attributes.
- systemd service-host integration used by validator activation, the root
  passive service and the repair controller (`systemctl`, `/proc/self/fd`
  re-execution, `/etc/machine-id` and boot ids). Run these on the Linux hosts
  that operate the services.

## Owner signing on macOS

The owner adapter supports macOS; the native SDK extension does not yet have a
macOS build. Build `bittensor_core` for macOS from the pinned Subtensor commit
with the locked Maturin procedure in [OWNER-LEDGER-SDK.md](OWNER-LEDGER-SDK.md),
pin its SHA256 and qualify it with the owner's actual Python, Ledger firmware
and app before signing. The Linux `linux_x86_64` ELF build is not usable on
macOS.

## Tests on macOS

Run tests with a physical temporary directory, because custody walks refuse the
`/var` alias that macOS uses for `TMPDIR`:

```sh
TMPDIR="$(cd "$TMPDIR" && pwd -P)" go test ./internal/... ./chain/ ./validator/ ./mainnet/
```

Tests that observe Linux-only facilities (sealed memfd engines, inotify read
census, cgroup magic) are in `*_linux_test.go` files and run on Linux only.
Linux parity can be checked from macOS by cross-compiling test binaries
(`GOOS=linux go test -c`) and running them in a Linux container on an ext4
volume as an unprivileged user.
