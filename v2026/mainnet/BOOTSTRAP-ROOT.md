# Executable local root-custody bootstrap phase

`sn-mainnet bootstrap plan/apply/resume` completes the local custody phase for
one independently approved action of an existing owned netuid-0 root seat. It
calls the existing [offline custody](ROOT-OFFLINE-CUSTODY.md) and
[root service](ROOT-SERVICE.md) owners, exports their exact public signing
packet, and imports a verified public native signature. It never loads a native
key, issues a signature, opens an RPC connection or starts the service loop.

This is a completed local operation, not completed mainnet bootstrap. Every
result keeps `chain_phases_pending: true` and `activation_ready: false`. The
separate [blocked review graph](PLAN.md) retains the complete launch scope and
cannot authorize this phase with its hash.

The [offline chain preparation](BOOTSTRAP-CHAIN.md) composes this owner and the
reserve CREATE custody owner with a retained trim review and two protected UR
role inputs. It has a separate accepted plan and durable progress journal;
chain effects and service activation remain pending.
Its v3 input additionally pins the root action approver independently and
requires a separate domain-separated approval of this complete child plan and
service configuration, including the observation allowance. The root command's
v1 plan and journals remain unchanged; their action approval alone is not that
full-service approval.

## Independent inputs and exact plan

The strict JSON configuration uses schema
`urnetwork-mainnet-bootstrap-root-config-v1`, `deployment_id`, `network`,
`run_directory`, and `root_service: {path, sha256}`. `network` independently
supplies `native_chain`, `genesis_hash` and `evm_chain_id: 964`. `root_service`
pins the exact bytes of the existing `rootServiceConfig`: its custody trust,
independently signed packet and finite observation allowance. The approval
public key and trust must come from the operator's actual approved authority;
accepting an arbitrary self-signed packet does not establish that authority.

Both inputs must be private regular files, at most 1 MiB each, in physical
owner-private directories. Paths are canonical and absolute, without symlinks
or substitutions. The precreated private run directory contains three distinct
direct-child journals: `bootstrap-root.json`, the approved custody state path,
and the approved service/action state path. Input files and all journal and
`.lock` paths must be distinct. The phase does not rewrite paths in an approval.

```sh
sn-mainnet bootstrap plan --config /secure/ur-mainnet/bootstrap-root.json > /secure/ur-mainnet/review/root-custody-plan.json
sn-mainnet bootstrap apply --config /secure/ur-mainnet/bootstrap-root.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$ROOT_CUSTODY_PLAN_HASH"
sn-mainnet bootstrap resume --config /secure/ur-mainnet/bootstrap-root.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$ROOT_CUSTODY_PLAN_HASH"
```

Plan is read-only. Its domain-separated schema
`urnetwork-mainnet-bootstrap-root-custody-plan-v1` seals exact config/service
byte hashes, paths, independent network and the complete signed packet. Apply
and resume recompute that plan before opening any journal. A review graph hash,
changed input, different run directory or EVM945 identity is rejected.
`network_effects` and `native_signing` are always false.

Apply creates the existing child owners once and returns their public packet.
`local_custody_complete: true` means both real journals exist and were checked;
`signature_status: awaiting-import` means no native signature has been retained.
An external custodian still needs current authority, global nonce ownership,
era, fee and device policy checks before signing. No CLI success provides those
facts or grants a native signing permission.

## Import and resume

The external public receipt uses the existing
`urnetwork-mainnet-root-offline-signature-v1` schema, `packet_hash` and
`signature_sr25519`. It contains no secret. Resume accepts a private regular
receipt bounded to 16 KiB and an exact raw-file SHA256 pin:

```sh
sn-mainnet bootstrap resume --config /secure/ur-mainnet/bootstrap-root.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$ROOT_CUSTODY_PLAN_HASH" --signature-file /secure/ur-mainnet/root-signature.json --signature-sha256 "$ROOT_SIGNATURE_FILE_SHA256"
```

The actual custody owner verifies the approved hotkey and native payload,
persists the original extrinsic bytes, and rejects a different signature even
over the same payload. Success reports `signature_status: retained` and the
extrinsic hash. It does not submit. Repeated resume returns the authoritative
child state, including service observations/broadcasts already consumed by a
separately qualified caller, without renewing their allowances.

Progress is a checked index, never a second nonce or financial journal. A
local exclusive lock serializes the phase. Interrupted progress publication or
stdout failure is reconciled from existing child journals on resume. A missing
or invalid completed progress, custody or service journal is not recreated.
Lost signed state cannot become an unsigned fresh allowance.

There is one bounded pre-child recovery case: an exact accepted-plan marker
whose initial claim was not completed. Resume may finish only while both child
files and their lock markers are absent, and progress is either absent or an
authenticated initial `claimed` record. It syncs a claim-complete marker before
any child can open. Partial markers, corrupt progress, later phases or any child
state in that window are refused. This is local crash recovery; hostile-host
rollback, distributed custody and global signer fencing remain external gates.

Exit 0 means the requested plan or local phase result was emitted. Input errors
exit 2; an unaccepted plan or retained ownership/state refusal exits 3; phase,
cancellation and output failures exit 1. After a failure retain every journal
and marker for resume or explicit investigation.

## Remaining launch work and qualification

The existing owned-RPC [submission adapter](ROOT-SUBMISSION.md) and root service
can consume these exact retained bytes. Production current-authority admission,
independently approved owned route/seat/custody and service activation still
need implementation or actual qualified inputs as documented by those owners.
This CLI does not activate those ports. Root basket weights address destination
netuids; the majority SN25 validator remains the standard evidence-based
`sn/validator` and cannot use this phase for an arbitrary miner reset.

Full bootstrap still needs executable safe partial owner trim with protected
identities and explicit residual generations; complete contract installation
including `STValidatorEvidence` and its anchor/coordinator binding; two eligible
UR validators and healthy operators; separately approved standard schema-3
10% native allocation / 90% recycle production; and monitored realized outcomes.
`Deploy.s.sol` alone omits the evidence journal. The testnet deployment manager
is bound to EVM945 and is not a mainnet execution adapter.

Deterministic local tests run command → real child owners → independently
approved owned HTTP fixture → canonical finalization → command resume. They
also interrupt initial claim, child progress, signature persistence and result
publication, reject altered authority/network and unsafe files, and verify that
resume consumes no new child allowance. These are synthetic fixture effects,
with no live keys, network calls, submissions or deployment. Exact qualification
commands and results are retained in the [evidence index](evidence/bootstrap-root-custody-20260928.md).
