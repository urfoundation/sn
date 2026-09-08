#!/usr/bin/env bash

# A foreground release gate owns a small bounded set of phase owners. Each
# Linux subreaper joins descendants across process groups/sessions and stays
# alive until acknowledged, preserving its identity through the final wait.

release_gate_process() {
  local line
  local -a fields
  [[ "$1" =~ ^[1-9][0-9]*$ ]] && IFS= read -r line < "/proc/$1/stat" || return 1
  read -r -a fields <<< "${line##*) }"
  RELEASE_GATE_PROCESS_STATE="${fields[0]}"
  RELEASE_GATE_PROCESS_GROUP="${fields[2]}"
  RELEASE_GATE_PROCESS_SESSION="${fields[3]}"
  RELEASE_GATE_PROCESS_START="${fields[19]}"
}

release_gate_owned_job() {
  local index="$1" pid="${release_gate_pids[$1]}"
  release_gate_process "$pid" || return 1
  [[ "$RELEASE_GATE_PROCESS_START" == "${release_gate_starts[index]:-}" &&
     "$RELEASE_GATE_PROCESS_GROUP" == "$pid" && "$RELEASE_GATE_PROCESS_SESSION" == "$pid" &&
     "$RELEASE_GATE_PROCESS_STATE" != Z ]]
}


# By default reserve half the effective CPU/memory for two concurrent gates.
# An explicitly single gate can set RELEASE_GATE_CONCURRENT_GATES=1.
# No test parallelism or deadline is changed; this bounds compiler/job pressure.
release_gate_effective_resources() {
  local quota period limit current available
  RELEASE_GATE_CPUS="$(nproc)" || return 1
  if [[ -r /sys/fs/cgroup/cpu.max ]]; then
    read -r quota period < /sys/fs/cgroup/cpu.max
    if [[ "$quota" != max && "$quota" =~ ^[0-9]+$ && "$period" =~ ^[1-9][0-9]*$ ]]; then
      limit=$((quota / period)); (( limit > 0 )) || limit=1
      if (( limit < RELEASE_GATE_CPUS )); then RELEASE_GATE_CPUS="$limit"; fi
    fi
  fi
  available="$(awk '$1 == "MemAvailable:" { printf "%.0f", $2 * 1024 }' /proc/meminfo)" || return 1
  RELEASE_GATE_MEMORY="${available%.*}"
  if [[ -r /sys/fs/cgroup/memory.max && -r /sys/fs/cgroup/memory.current ]]; then
    read -r limit < /sys/fs/cgroup/memory.max
    read -r current < /sys/fs/cgroup/memory.current
    if [[ "$limit" =~ ^[0-9]+$ && "$current" =~ ^[0-9]+$ ]]; then
      available=$((limit - current)); (( available > 0 )) || available=1
      if (( available < RELEASE_GATE_MEMORY )); then RELEASE_GATE_MEMORY="$available"; fi
    fi
  fi
}

release_gate_jobs_init() {
  local command cpu_slots memory_slots requested reservations
  for command in setsid python3 stat nproc mkfifo timeout; do command -v "$command" >/dev/null || return 1; done
  [[ -d /proc/self ]] || { echo 'release gate jobs require Linux process identities' >&2; return 1; }
  set +m
  release_gate_jobs_script="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)/release-gate-jobs.sh"
  release_gate_root="$(mktemp -d "${TMPDIR:-/tmp}/urnetwork-release-gate.XXXXXXXX")" || return 1
  release_gate_root="$(cd -- "$release_gate_root" && pwd -P)" || return 1
  declare -ga release_gate_pids=() release_gate_starts=() release_gate_labels=() release_gate_pending=() release_gate_ack_fds=()
  release_gate_active=0 release_gate_result=0 release_gate_services_cleaned=0
  trap 'release_gate_jobs_exit "$?"' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  if [[ "${SIM_TESTNET_LIVE_DEPENDENCIES:-0}" != 0 ]]; then
    echo 'live dependency opt-in belongs to separate campaign qualification, not an isolated gate' >&2
    return 1
  fi
  mkdir -m 700 "$release_gate_root/logs" "$release_gate_root/full-out" "$release_gate_root/full-cache" \
    "$release_gate_root/slither-out" "$release_gate_root/slither-cache" || return 1
  # gencontracts validates immutable declaration order beside its artifact
  # input. This is a read-only reference to the same source-freeze checkout.
  ln -s -- "$sn_repo/evm/src" "$release_gate_root/src" || return 1
  ln -s -- "$sn_repo/evm/lib" "$release_gate_root/lib" || return 1
  mkfifo -m 600 "$release_gate_root/completions" || return 1
  exec {release_gate_completion_fd}<>"$release_gate_root/completions"
  release_gate_effective_resources || return 1
  reservations="${RELEASE_GATE_CONCURRENT_GATES:-2}"
  [[ "$reservations" == 1 || "$reservations" == 2 ]] || { echo 'RELEASE_GATE_CONCURRENT_GATES must be 1 or 2' >&2; return 1; }
  cpu_slots=$((RELEASE_GATE_CPUS / (4 * reservations))); memory_slots=$((RELEASE_GATE_MEMORY / (4294967296 * reservations)))
  release_gate_limit="$cpu_slots"
  if (( memory_slots < release_gate_limit )); then release_gate_limit="$memory_slots"; fi
  (( release_gate_limit > 0 )) || release_gate_limit=1
  requested="${RELEASE_GATE_JOBS:-$release_gate_limit}"
  [[ "$requested" =~ ^[1-9][0-9]{0,4}$ ]] && (( requested <= release_gate_limit )) || {
    echo "RELEASE_GATE_JOBS must be between 1 and the effective limit $release_gate_limit" >&2; return 1;
  }
  release_gate_limit="$requested"
  release_gate_job_cpus=$((RELEASE_GATE_CPUS / (reservations * release_gate_limit)))
  (( release_gate_job_cpus > 0 )) || release_gate_job_cpus=1
  if (( release_gate_job_cpus > 4 )); then release_gate_job_cpus=4; fi
  export GOMAXPROCS="$release_gate_job_cpus" GOFLAGS="${GOFLAGS:-} -p=$release_gate_job_cpus"
  export RAYON_NUM_THREADS="$release_gate_job_cpus" CARGO_BUILD_JOBS="$release_gate_job_cpus"
  export FOUNDRY_OUT="$release_gate_root/full-out" FOUNDRY_CACHE_PATH="$release_gate_root/full-cache"
  export SLITHER_FOUNDRY_OUT="$release_gate_root/slither-out" SLITHER_FOUNDRY_CACHE_PATH="$release_gate_root/slither-cache"
  export release_gate_root sn_repo workspace
  source "$workspace/server/local/release-gate-services.sh" || return 1
  printf '[release gate] private state %s; ready jobs=%s; CPUs/job=%s\n' "$release_gate_root" "$release_gate_limit" "$release_gate_job_cpus"
}


release_gate_start() {
  local label="$1" function="$2" index pid deadline ack_fd
  [[ "$label" =~ ^[a-z0-9-]+$ ]] && declare -F "$function" >/dev/null || return 1
  while (( release_gate_active >= release_gate_limit )); do release_gate_wait_one || return 1; done
  index="${#release_gate_pids[@]}"
  mkdir -m 700 "$release_gate_root/job-$index" "$release_gate_root/job-$index/tmp" || return 1
  mkfifo -m 600 "$release_gate_root/job-$index/ack" || return 1
  exec {ack_fd}<>"$release_gate_root/job-$index/ack"
  export -f "$function"
  setsid python3 "${release_gate_jobs_script%/*}/release-gate-child.py" "$release_gate_root" "$index" "$function" > "$release_gate_root/logs/$label.log" 2>&1 &
  pid=$!
  release_gate_pids[index]="$pid" release_gate_labels[index]="$label" release_gate_pending[index]=1 release_gate_ack_fds[index]="$ack_fd"
  release_gate_active=$((release_gate_active + 1))
  release_gate_process "$pid" || return 1
  release_gate_starts[index]="$RELEASE_GATE_PROCESS_START"
  deadline=$((SECONDS + 10))
  until [[ -f "$release_gate_root/job-$index/ready" ]] && release_gate_owned_job "$index"; do
    (( SECONDS < deadline )) || return 1
    sleep 0.01
  done
  printf '[release gate] started %s (pid %s)\n' "$label" "$pid"
}


release_gate_reap_job() {
  local index="$1" pid="${release_gate_pids[$1]}" deadline=$((SECONDS + 10)) exit_deadline="${2:-0}" ack_fd
  if (( exit_deadline > 0 && exit_deadline < deadline )); then deadline="$exit_deadline"; fi
  while release_gate_process "$pid" && [[ "$RELEASE_GATE_PROCESS_START" == "${release_gate_starts[index]:-}" && "$RELEASE_GATE_PROCESS_STATE" != Z ]]; do
    (( SECONDS < deadline )) || return 1
    sleep 0.01
  done
  RELEASE_GATE_JOB_EXIT=0
  wait "$pid" || RELEASE_GATE_JOB_EXIT=$?
  release_gate_pending[index]=0
  release_gate_active=$((release_gate_active - 1))
  ack_fd="${release_gate_ack_fds[index]}"
  exec {ack_fd}>&-
}

release_gate_wait_one() {
  local index status extra deadline="${1:-0}"
  while :; do
    if (( deadline > 0 && SECONDS >= deadline )); then
      echo "release gate: canceled completion deadline expired; refusing an unproven cleanup" >&2
      return 1
    fi
    if IFS=' ' read -r -t 1 index status extra <&"$release_gate_completion_fd"; then
      [[ "$index" =~ ^(0|[1-9][0-9]*)$ && "$status" =~ ^(0|[1-9][0-9]{0,2})$ && -z "$extra" ]] || return 1
      (( ${#index} < 9 && index < ${#release_gate_pids[@]} )) || return 1
      [[ "${release_gate_pending[index]:-0}" == 1 ]] && (( status <= 255 )) || return 1
      release_gate_owned_job "$index" || return 1
      printf 'joined\n' >&"${release_gate_ack_fds[index]}" || return 1
      release_gate_reap_job "$index" "$deadline" || return 1
      if (( RELEASE_GATE_JOB_EXIT != status )); then
        printf 'release gate: worker/owner exit mismatch: %s/%s\n' "$status" "$RELEASE_GATE_JOB_EXIT" >&2
        status="$RELEASE_GATE_JOB_EXIT"
        (( status != 0 )) || status=125
      fi
      printf '[release gate] joined %s (exit %s); log %s\n' "${release_gate_labels[index]}" "$status" "$release_gate_root/logs/${release_gate_labels[index]}.log"
      if (( status != 0 && release_gate_result == 0 )); then release_gate_result="$status"; fi
      return 0
    fi
    for index in "${!release_gate_pids[@]}"; do
      if [[ "${release_gate_pending[index]}" == 1 ]] && ! release_gate_owned_job "$index"; then
        echo "release gate: child owner disappeared without completion; refusing an unproven cleanup" >&2
        return 1
      fi
    done
  done
}

release_gate_join() {
  while (( release_gate_active > 0 )); do release_gate_wait_one || return 1; done
  return "$release_gate_result"
}

release_gate_complete() {
  local status=0
  release_gate_join || status=$?
  (( release_gate_active == 0 )) || return 1
  if release_gate_services_cleanup; then
    release_gate_services_cleaned=1
  else
    (( status != 0 )) || status=1
  fi
  return "$status"
}

release_gate_jobs_exit() {
  local status="$1" index joined=1 owned deadline=$((SECONDS + 20))
  trap - EXIT INT TERM
  set +e
  for index in "${!release_gate_pids[@]}"; do
    if [[ "${release_gate_pending[index]}" == 1 ]]; then
      if release_gate_owned_job "$index"; then
        kill -TERM -- "${release_gate_pids[index]}" || joined=0
      else
        joined=0
      fi
    fi
  done
  # A missing completion keeps its pending slot and makes cleanup unproven,
  # but must not prevent another retained owner from acknowledging and joining.
  # The existing child cleanup ten seconds plus owner reap ten seconds bound
  # EXIT only; a live gate may keep normally completed owners awaiting its ack.
  while (( release_gate_active > 0 && SECONDS < deadline )); do
    owned=0
    for index in "${!release_gate_pids[@]}"; do
      if [[ "${release_gate_pending[index]}" == 1 ]]; then
        if release_gate_owned_job "$index"; then
          owned=1
        else
          joined=0
        fi
      fi
    done
    (( owned == 1 )) || break
    release_gate_wait_one "$deadline" || joined=0
  done
  (( release_gate_active == 0 )) || joined=0
  if (( status == 0 && release_gate_result != 0 )); then status="$release_gate_result"; fi
  if (( joined == 1 )); then
    if [[ "$release_gate_services_cleaned" != 1 ]] && declare -F release_gate_services_cleanup >/dev/null; then
      release_gate_services_cleanup || { (( status != 0 )) || status=1; }
    fi
  else
    echo "release gate: not all child identities joined; private services retained for inspection" >&2
    (( status != 0 )) || status=1
  fi
  printf '[release gate] retained private logs/state: %s\n' "$release_gate_root"
  exit "$status"
}
