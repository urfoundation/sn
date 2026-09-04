#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0
#
# Smoke-test every sn CLI target built by build/all/run.sh. The output is
# discarded: this script is intended to catch target-specific compile failures
# before the release pipeline creates and publishes the versioned modules.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
cd "$here"

build_target() {
    local command="$1"
    local osarch="$2"
    local target_os="${osarch%/*}"
    local target_arch="${osarch#*/}"
    local -a target_env=(
        GOEXPERIMENT=greenteagc
        CGO_ENABLED=0
        GOOS="$target_os"
        GOARCH="$target_arch"
    )

    case "$target_arch" in
        mips*) target_env+=(GOMIPS=softfloat) ;;
    esac

    echo "== build $command ($target_os/$target_arch)"
    env "${target_env[@]}" go build -trimpath -o /dev/null "./cli/$command"
}

miner_targets=(
    linux/arm64 linux/arm linux/amd64 linux/386
    linux/mips linux/mipsle linux/mips64 linux/mips64le
    darwin/arm64 darwin/amd64 windows/arm64 windows/amd64
)

operator_targets=(
    darwin/arm64 darwin/amd64 linux/amd64 linux/arm64
)

for target in "${miner_targets[@]}"; do
    build_target miner "$target"
done

for command in validator snclaim; do
    for target in "${operator_targets[@]}"; do
        build_target "$command" "$target"
    done
done

echo "== sn CLI builds OK"
