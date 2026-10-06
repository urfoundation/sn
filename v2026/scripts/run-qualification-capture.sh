#!/usr/bin/env bash
# Enter the declared source tree, validate its relative manifest, then run the
# capture-local immutable body through Bash without relying on executable mode.
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo 'usage: bash run-qualification-capture.sh SOURCE_ROOT SOURCE_MANIFEST FROZEN_RUNNER' >&2
  exit 64
fi

qualification_source=$1
qualification_manifest=$2
qualification_runner=$3
for qualification_path in "$qualification_source" "$qualification_manifest" "$qualification_runner"; do
  if [[ "$qualification_path" != /* ]]; then
    echo 'qualification paths must be absolute' >&2
    exit 64
  fi
done
if [[ ! -d "$qualification_source" || ! -f "$qualification_manifest" || ! -f "$qualification_runner" ]]; then
  echo 'qualification source, manifest or frozen runner is missing' >&2
  exit 66
fi

cd -- "$qualification_source"
sha256sum --check --strict --quiet "$qualification_manifest"
exec bash -- "$qualification_runner"
