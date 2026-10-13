#!/usr/bin/env bash

# Environment adapter for test/build commands. Ordinary Go tests remain
# portable; release gates and with-test-storage.sh opt into explicit storage.
sn_test_storage_init() {
  local storage_root="${SN_TEST_STORAGE_ROOT:-}" command_root
  if [[ -z "$storage_root" ]]; then
    storage_root=/mnt/data/sn-testnet
  fi
  # A nested command inherits the selected root. It must still check this
  # mount; a previously exported default is not permission to fall back.
  if [[ "$storage_root" == /mnt/data || "$storage_root" == /mnt/data/* ]]; then
    if ! mountpoint -q /mnt/data; then
      echo 'test storage: /mnt/data is not mounted; attach the data volume or explicitly set SN_TEST_STORAGE_ROOT to an absolute storage directory' >&2
      return 1
    fi
  fi
  if [[ "$storage_root" != /* ]]; then
    echo 'test storage: SN_TEST_STORAGE_ROOT must be absolute' >&2
    return 1
  fi
  # Preserve the caller's output modes. Only storage/owner directories are
  # private; changing the command's umask breaks portable artifact fixtures.
  (umask 077; mkdir -p -- "$storage_root/temp" "$storage_root/gocache" "$storage_root/gomodcache" "$storage_root/cache") || return 1
  storage_root="$(cd -- "$storage_root" && pwd -P)" || return 1
  command_root="$(mktemp -d "$storage_root/temp/command.XXXXXXXX")" || return 1
  mkdir -m 700 -- "$command_root/tmp" "$command_root/gotmp" || return 1
  export SN_TEST_STORAGE_ROOT="$storage_root" SN_TEST_COMMAND_ROOT="$command_root"
  export TMPDIR="$command_root/tmp" GOTMPDIR="$command_root/gotmp"
  export TMP="$TMPDIR" TEMP="$TMPDIR"
  export GOCACHE="$storage_root/gocache" GOMODCACHE="$storage_root/gomodcache" XDG_CACHE_HOME="$storage_root/cache"
  export CARGO_TARGET_DIR="$command_root/cargo-target"
  export RUNTIME_METADATA_PROBE_TARGET_DIR="$storage_root/cache/runtime-metadata-probe-target"
  printf '[test storage] root=%s; owner=%s; go-cache=%s; go-modules=%s\n' "$storage_root" "$command_root" "$GOCACHE" "$GOMODCACHE" >&2
}
