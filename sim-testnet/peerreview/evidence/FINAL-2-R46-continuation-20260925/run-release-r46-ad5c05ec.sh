#!/usr/bin/env bash
# Qualified provisional owner only; retained R45 failure and supervisor persist.
set -euo pipefail
set -o noclobber
candidate_dir=/mnt/data/sn-testnet/qualification/r46-provisional-resume-20260925
binary="$candidate_dir/build/sim-testnet-r46-ad5c05ec"
state_dir=/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4
config=/mnt/data/sn-testnet/qualification/release37-release-20260923/rate-config/sim-testnet/testnet.yml
plan_hash=0x8bb92697db8f2164e46f6e58848d3407e509382fb61550b919f1d55391ad480e
printf '%s  %s\n' d583a85b8085199527ed5c87a5f7cb278e36c3856e17d8bbca3958ab0aeb7cd4 "$candidate_dir/launch-inputs.sha256" | sha256sum --check --status
sha256sum --check --status "$candidate_dir/launch-inputs.sha256"
python3 "$candidate_dir/preflight.py" > "$candidate_dir/preflight-launch.json"
exec "$binary" scenario --name release-1.0 \
  --config "$config" \
  --state-dir "$state_dir" \
  --sn-repo /home/by/urnetwork/sn \
  --server-repo /home/by/urnetwork/server \
  --operator-proxy-repo /home/by/urnetwork/operator-proxy \
  --vault-repo /home/by/urnetwork/vault \
  --platform-config-repo /home/by/urnetwork/config \
  --owned-rpc-authority 192.168.1.162:9944 \
  --provisional-resume \
  --format json \
  --apply \
  --plan-hash "$plan_hash" \
  > "$candidate_dir/release-r46.stdout.json"
