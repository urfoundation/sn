#!/usr/bin/env bash
# Bootstrap storage before the Go driver/compiler can create temporary files.
set -euo pipefail

if [[ $# -eq 0 ]]; then
  echo 'usage: with-test-storage.sh COMMAND [ARG ...]' >&2
  exit 64
fi
source "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/test-storage.sh"
sn_test_storage_init
# Keep the actual command's status and process/signal ownership unchanged.
exec "$@"
