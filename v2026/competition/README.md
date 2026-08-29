# sim-latency competition client

This dependency-free reference package implements the miner/operator side of
[`../api/competition.yml`](../api/competition.yml): hidden-round generation,
submit-and-poll scoring, the structural pre-screen, and score-schema-1
normalization. The server repeats every screen and is the authority.

`GET /competition/info` publishes every non-secret run field: hardware and
host-qualification identity, simulator/scorer digests, provider/client/rate
scale, quality window, host/shard counts, loopback ports, all lifecycle
durations, impairment state, replicate count, takeover margin, queue limit,
and evaluation timeout. Zero or implicit production values are rejected by the
server.

The editable surface is the exact `allowed_paths` list returned by
`GET /competition/info`, minus every `forbidden_paths` entry. The evaluator
also hard-forbids the simulator/harness, scorer, stats/accounting, migrations,
generated/build files, module sums, vendor, CI, payments/contracts, binary
patches, path traversal, file creation/deletion, symlinks, submodules, renames,
copies, and mode changes.

```python
from competition.runner import CompetitionRunner
from competition.screener import screen_file

screened = screen_file(
    "candidate.patch",
    max_patch_bytes=262144,
    allowed_paths=("connect/resident_contract_manager.go",),
    forbidden_paths=(
        "connect/sim-latency/**",
        "stats/**",
        "db_migrations.go",
        "db_migrations_*.go",
        "go.mod",
        "go.sum",
        "vendor/**",
        ".github/**",
    ),
)
runner = CompetitionRunner("https://api.bringyour.com", "SUBMITTER_TOKEN")
job = runner.run("ROUND_UUID", screened.canonical_bytes.decode(), timeout_seconds=7200)
```

After `reveal_at`, authenticate the byte-identical workload against the digest
that was published when the round was generated:

```python
from competition.client import CompetitionClient

client = CompetitionClient("https://api.bringyour.com", "SUBMITTER_TOKEN")
providers = client.download_revealed_workload(
    "ROUND_UUID", round_commitment.providers_sha256,
)
open("providers.yml", "xb").write(providers)
```

`competition.reveal.verify_reveal` separately verifies the CSPRNG-seed
commitment and providers digest, then returns the exact positive 63-bit seed
passed to `sim-latency init`. Active-round models intentionally contain no seed
field.

Lower raw latency is better. The deterministic public normalization is
`clamp(100 * baseline_raw / candidate_raw, 1, 200)`. A placeable submission
takes over only when `candidate_raw <= baseline_raw * (1 - takeover_margin)`.
The API withholds raw/per-replicate diagnostics until reveal; an active miner
receives normalized score, placeability, gate booleans, and a typed error.

Run the reference checks with:

```bash
cd sn
python3 -m unittest discover -s competition/tests -v
```

Reproduction requires the revealed seed and downloaded providers file, pinned
`BASE_SHA`, exact canonical patch bytes/hash, providers hash, score
schema/scorer version, frozen run
policy, digest-pinned evaluator image, signed baseline, every retained
replicate artifact, and the immutable accounting/resource reports. Absolute
latency from another machine is not comparable; relative improvement is the
portable signal. Production results come only from the single authoritative
12-physical-core evaluation host, with 10 cores assigned to the evaluation and
2 reserved for management and cleanup.

Typed 4xx errors (`auth` or `submission`) require changing credentials or the
patch/request. Typed retriable 5xx errors and `infrastructure` job failures are
retried by the service under the same `(round_id, canonical_patch_hash)` cache
identity; they never grant a new noise draw. Keep the bearer token out of patch
files, logs, and command lines. Fees, round/reveal timing, licensing, season end,
and grand-final policy are published by the competition operator before launch.
