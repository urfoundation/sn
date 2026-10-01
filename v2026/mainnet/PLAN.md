# Signer-free bootstrap planning

`sn-mainnet plan` produces a reviewable dependency graph, with every action
explicitly blocked. It does not produce a signed or executable launch plan,
load a signer, make an RPC request, inspect a live filesystem release, or start
services. Its hash must never be accepted as executable authority. The separate
[local root-custody phase](BOOTSTRAP-ROOT.md) now implements `bootstrap
plan/apply/resume` with its own schema, exact approved packet and durable owners;
it does not execute this review graph or complete the chain bootstrap.

Before an exact observation and separately declared target identity are available:

```sh
sn-mainnet plan --outline > bootstrap-outline.json
```

The outline has `status: unbound_outline`, no bound network or block, no content
seal and all requirements marked `missing`. It keeps the work visible without
relabeling the historical Snow testnet EVM945 observation as mainnet evidence.

For an exact bound review, supply the intended mainnet identity separately
from the snapshot:

```sh
sn-mainnet plan --config /secure/ur-mainnet/plan-config.json > blocked-plan.json
```

The implemented config is strict **JSON**, distinct from the future executable
YAML design in [MAINNET.md](MAINNET.md). The following incomplete template is
intentionally rejected until the operator supplies the real values; `null`
does not mean automatic discovery or approval:

```json
{
  "schema": "urnetwork-mainnet-plan-config-v1",
  "deployment_id": "ur-sn25-launch",
  "netuid": 25,
  "network": {
    "native_chain": null,
    "genesis_hash": null,
    "evm_chain_id": 964
  },
  "snapshot": {"path": "finalized-snapshot.json", "sha256": null},
  "source_lock": {"path": "source-lock.json", "sha256": null},
  "release": {"path": "release-input.json", "sha256": null}
}
```

Every `sha256` is `sha256:` followed by 64 lowercase hexadecimal digits over
the exact file bytes, including any final newline. It is different from an
artifact's domain-separated `content_hash`. The snapshot must come from
`finalized-snapshot`, with one shared native hash; two independently sampled
runtime/mapping files cannot be substituted. The source lock is the existing
`source-lock` JSON. The config's network is separately supplied, never copied
or defaulted by the planner from either file. EVM945 and mismatching genesis
are refused. The plan remains blocked even when the expected identity matches.
A declared review target is not independent network approval: `owned-rpc` and
`runtime-authority` remain missing unless separate review evidence is supplied,
and supplied files still remain unvalidated. The
[October 1 unsigned preparation](evidence/public-unsigned-preparation-20261001.md)
uses the previously declared mainnet target and a fresh Rao archive capture
without inventing approval. Signing and execution still require independent
network/runtime/source authority and all separate phase gates.

The release-input JSON binds both internal seals and all runtime artifacts:

```json
{
  "schema": "urnetwork-mainnet-release-input-v1",
  "deployment_id": "ur-sn25-launch",
  "netuid": 25,
  "snapshot_content_hash": null,
  "source_lock_content_hash": null,
  "runtime_version": {
    "specName": null,
    "specVersion": null,
    "transactionVersion": null,
    "stateVersion": null
  },
  "runtime_code_hash": null,
  "runtime_metadata_hash": null,
  "review_inputs": []
}
```

Optional `review_inputs` entries have `requirement`, `path` and `sha256` fields.
Requirement IDs are listed by `plan --outline`; unknown or duplicate IDs are
rejected. For example, an exact retained final testnet report can be supplied
under `testnet-closure`. All these files remain `supplied_unvalidated`: the
planner checks byte identity, not their domain semantics, approval signatures
or claimed outcomes. A manifest containing `approved: true` cannot close a gate.

File paths resolve relative to the containing config or release manifest.
They are literal local paths: no URL download, environment expansion or shell
execution. Empty files, nonregular files and final-component symlinks are
refused. Config/source/release/supplemental manifests are each bounded to 1 MiB;
at most 32 supplemental inputs are accepted. The combined snapshot is bounded
to 26 MiB to accommodate maximum retained code/metadata hex plus framing.
JSON rejects duplicate keys, unknown fields, overflow and trailing documents.

The bound output has schema `urnetwork-mainnet-blocked-plan-v2`, `status: blocked`,
`apply_authority: false`, `activation_ready: false`, exact raw-file and internal
hash references, native/EVM hashes and their independently decoded heights.
Runtime byte hashes, the native header commitment, Frontier digest and raw EVM
RLP linkage are recomputed locally. This cannot prove owned-RPC finality,
storage state, source-to-Wasm mapping, clean build provenance or external approval.
The input source lock records Git claims; this command does not rerun Git.

The deterministic graph covers release qualification, authority, miner UID
reset, custody contract deployment, subnet registration, separate root and UR
validator services, monitoring, emission activation and acceptance. Root service
and operations preparation can proceed alongside subnet reset/deployment once
their shared authority prerequisites are satisfied. Activation depends on all
three branches. The root role never counts toward the required **two distinct
UR validators and two healthy operators**.

Schema v2 separates preconditions from action `postconditions`. A requirement's
`produced_by` names its sole producer, which must precede every consumer in the
dependency graph. Contract inputs contain expected getter values; observed
getter proofs follow installation. UR service inputs contain approved roles,
permits and configuration; revealed/applied rows follow activation. Approved
10/90 mechanics and rounding are activation inputs; realized native economics
are acceptance outputs. Validation rejects a same-action or downstream fact
used as a prerequisite. Historical v1 outlines remain review evidence only.
The [current v2 outline](evidence/blocked-plan-outline-v2-20260928.json) contains
ten review actions and 30 input/output requirements, all explicitly unvalidated.

The operator has SN25 owner keys but no chain-Root administrative origin. The
planned native reset therefore seeks the strongest safe owner-authorized trim,
with exact before/after hotkey generations, protected survivors and a named
disposition for every old miner that remains. This blocked graph does not infer
that an owner trim can remove all miners, and its `reset-miner-uids` action is
not executable authority. See [the reset design](MAINNET.md#exact-uid-census-and-reset).
The separate signer-free [owner-trim planner](SUBNET-CENSUS.md#best-effort-owner-trim)
now ranks partial owner-key choices and records residual generations from one
authenticated census. Its prediction cannot satisfy this graph's reset or
execution authority requirement. The [current offline outline](evidence/blocked-plan-outline-owner-trim-20260927.json)
incorporates the partial-trim description under v1; it remains historical.

The economic target is fixed to the requested **1/10 of native miner allocation
before withholding**, with **9/10 owner-recycle**, observed-native-target assurance
and no reserve credit. This is a requested policy, not proof that Yuma or an
installed contract implements it. Activation requires approved allocation,
quantization and owner-mode policy. Actual native allocation and realized
remainder evidence are postconditions collected after activation.

Exact missing interfaces remain explicit: semantic validators for runtime
authority, release/artifacts, role/custody/limit manifests, complete generation
census and reset capability, actual deployment/registration payloads, existing
root seat/strategy and production current-authority admission, UR permits/CRv4 quorum, migration
cutover, native economic outcomes, monitoring/on-call, and final acceptance.
Present-day execution also needs a fresh boundary, exact per-action origin,
nonce/address/postconditions, durable receipt/nonce ownership, lifetime ledger
and separate signed phase/cap/expiry authorization. The planner invents none.
The existing root service/custody/submission owners and the executable local
phase are implemented; actual live authority and the remaining chain bootstrap
orchestration are distinct outstanding work.

Identical input bytes produce identical bound output. `content_hash` is SHA256
of `urnetwork-mainnet-blocked-plan-v2` plus a zero byte plus canonical Go JSON
with `content_hash` set to the empty string. No wall-clock time or mutable
source tree enters the hash. Exit 0 means only the review JSON was emitted;
identity mismatch exits 3, malformed/missing input exits 2 with no partial output,
and output failure exits 1.
