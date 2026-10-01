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

    # mips64/mips64le read GOMIPS64 and ignore GOMIPS (https://go.dev/wiki/GoMips).
    case "$target_arch" in
        mips|mipsle) target_env+=(GOMIPS=softfloat) ;;
        mips64|mips64le) target_env+=(GOMIPS64=softfloat) ;;
    esac

    echo "== build $command ($target_os/$target_arch)"
    case "$target_arch" in
        mips*)
            # MIPS routers commonly have no FPU, so a hardfloat binary crashes
            # there. Check the float mode the toolchain recorded.
            local out
            out="$(mktemp "${TMPDIR:-/tmp}/sn-build.XXXXXX")"
            env "${target_env[@]}" go build -trimpath -o "$out" "./cli/$command"
            if ! go version -m "$out" | grep -Eq 'GOMIPS(64)?=softfloat'; then
                echo "error: $command ($target_os/$target_arch) is not softfloat" >&2
                rm -f "$out"
                return 1
            fi
            rm -f "$out"
            ;;
        *)
            env "${target_env[@]}" go build -trimpath -o /dev/null "./cli/$command"
            ;;
    esac
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
