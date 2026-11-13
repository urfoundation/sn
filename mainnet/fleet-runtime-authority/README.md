# Mainnet fleet runtime authority: how one gets approved and published

[`miner/FLEET-MAINNET-RUNTIME.md`](../../miner/FLEET-MAINNET-RUNTIME.md) is the
contract: what the document holds and what `provider fleet` does with it. This
says how one comes to exist. **None is approved or published today**, and there is
no date.

## What goes in it

| Field | Value source | Independent check |
| --- | --- | --- |
| `schema`, `runtime_review_scope` | fixed in `miner/fleet_mainnet_runtime.go` | |
| `native_chain`, `genesis_hash` | `system_chain`, `chain_getBlockHash(0)` | a second endpoint, and `validator/mainnet-profile.json` |
| `evm_chain_id`, `netuid`, `coordinator` | `mainnet/MAINNET.md`, "As launched" | `eth_chainId`, the coordinator's `netuid()`, and the manifest. The coordinator is lowercase hex: a checksummed address is refused. |
| `runtime_source_commit` | upstream's tag for the spec version | upstream's `deployment.json` and srtool digest, and an srtool rebuild |
| `runtime_version` | `state_getRuntimeVersion` at finalized | the metadata probe, from the Wasm |
| `runtime_code_hash` | BLAKE2b-256 of `:code` at finalized | upstream's release asset and `deployment.json` |
| `runtime_metadata_hash` | BLAKE2b-256 of `state_getMetadata` at finalized | `tools/runtime-metadata-probe` on upstream's Wasm |
| `runtime_review_sha256` | SHA-256 of the signed-off review | the reviewer's own record |

`check.py` compares a document with both endpoints and with upstream's release.
Matching does not approve anything: an endpoint observation or a matching hash is
not the review.

## Who approves it

1. An independent reviewer signs off the review, covering everything
   `FLEET-MAINNET-RUNTIME.md` lists: source-to-Wasm provenance and the consumed
   interface. Its SHA-256 goes in the document. The review stays outside this
   repository until the owner decides otherwise.
2. The owner approves the exact document bytes and their SHA-256.

## How it is published

Only after the owner's approval, and together:

1. The document in this directory as `runtime-<spec>.json`, merged to sn `main`.
   It is not a draft in this tree until then.
2. Its SHA-256 and link in `FLEET-MAINNET-RUNTIME.md`, the ur.xyz miner docs and
   the ur.io Top 200 page, so an operator can compare the digest they computed
   with one they did not fetch from the same place.

An operator passes both with `--mainnet-runtime-authority=<absolute path>` and
`--mainnet-runtime-authority-sha256=<64 lowercase hex digits>`.

## When the runtime changes

Every runtime upgrade stops `provider fleet` writes and status until a new
document is reviewed and published: no runtime is auto-adopted. Mainnet went from
runtime 475 at launch on 2026-10-08 to 486 by 2026-10-26, so plan the review to
follow each upstream release. The `urnetwork-mainnet-fleet-runtime-authority-v2` catalog
(`miner/fleet_runtime_catalog.go`) lists up to 8 reviewed runtimes with the
purposes each is reviewed for, which is the form to use once more than one runtime
matters.
