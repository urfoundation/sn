# Safe current-storage proof qualification

**Scoped qualification complete.** All eleven new and ten adjacent roots pass normally
and under race detection, for 42 positive root executions. All ten normal
controls and exactly five selected race controls are causal. The complete
source/dependency evidence is sealed. This qualifies the read-only proof and
proposal boundary; it does not approve the proposed policy or activate submission.

Frozen source is `aa9f715b07dd35b8e1c0eee269b7e522443c1724`, tree
`f56b48a97269c35a134427f7a4ae3954fe4145cd`, based exactly on qualified runtime
source `3d526830551ec2d8e7b0fc01f7bac5baa78e1789`. The implementation worktree is
`/home/by/urnetwork/sn-successor-safe-current-proof-owner-20260930`. Ten new files
add the verifier, observations, signed proposal, fixtures and proposal document;
existing source/test/module bytes are unchanged. Earlier qualification receipts
remain immutable and scoped to their own source.

## Qualified property and explicit limits

The [proposal](../SAFE-CURRENT-AUTHORITY-PROPOSAL.md) binds one exact Safe's
complete native `EVM.AccountStorages` prefix, native runtime code, published
proxy/singleton code and native code metadata to a SCALE-authenticated finalized
header. The collector requests fourteen known keys, then proves complete prefix
coverage by traversing every intersecting proof branch. Missing nodes or values
refuse; a supplied hidden slot also refuses. It admits only the singleton, exact
three-owner links, threshold two, exact nonce and empty module sentinel. Every
other storage word must be absent, including otherwise harmless historical
approved-hash or signed-message storage.

The mathematical proof does not establish deployment/initialization history,
past delegatecalls, independent finality or pending completeness. The owned RPC
asserts finality. Its separately refreshed pending calls expose no complete
overlay proof or atomic snapshot token. The scoped pending helper checks known
code/words after expensive proof work but reports `complete_pending_verified`
and `send_authorized` as false. A newly introduced unknown pending orphan can
escape those finite reads; the deterministic fixture preserves this limitation.

The independent Ed25519 proposal binds original execution/canonical authority,
the complete retained runtime tip, an already approved runtime, exact Safe
profile, distinct pinned review evidence and explicit current-only policy text.
It cannot import custody, satisfy the existing complete-history interface or
enable public `--submit`. A separate signed immutable policy journal, exact
event binding and qualified production capability must be implemented and
reviewed before seeking policy approval. The original history statement remains
retained; current proof never becomes a claim that its history was proven.

## Independent fixtures and author handoff

The unchanged SDK oracle has SHA-256
`b875b9eb1aa497233f68bb4cae02b4331320fcaeeaaefbefe867658f2aebaecd`.
Its eighteen raw StorageProof vectors come from the pinned Rust SDK
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`. The separate mutable Go fixture
encoder must reproduce all nine layout-one SDK roots. It is not treated as an
independent codec oracle by itself. Reviewed native storage source is Subtensor
`67dcf7f791dc495064c293f080a0702cb433e51e`.

Actual published Safe 1.4.1/1.5.0 Safe/SafeL2 execution supplies layout and
owner/module authorization facts. Orphan fixtures demonstrate that ordinary
getters accept hidden authority while complete-prefix admission refuses it.
Other fixtures cover omitted intersecting branches and external values,
malformed proofs, extra words, root/header/snapshot substitution, runtime and
code/metadata substitution, advancing finalized heads, actual canonical
mismatch, independent signatures and scoped pending changes.

Astra max owns implementation, debugging, compile-only checks and vet. Sol
medium owns behavioral tests and controls. Author formatting/compile-only/vet
pass; all ten isolated mutations also compile and pass vet. No author behavioral
tests, live RPC, real signing operation or transaction were performed.

The author handoff is `/tmp/safe-current-storage-handoff-20260930`. Its immutable
16-file `PAYLOAD.SHA256SUMS` hashes to
`c40714d803cbada399456f7401863d22aa745fda06ab1288c2159a091ad3f5d0`.
The supplementary 38-file `SHA256SUMS`, including all mutation compile/vet logs,
hashes to `dcf98ddbe42fc6203beb35fe0de45f923213ce2231cc3e5682aaaf8963a80645`.
`CONTROLS.json` hashes to
`69ce67292373e497d609e483a0131a91928c200c8cc0cbdb05567cafcc56efee`;
`TEST-PLAN.json` hashes to
`e1f81dc739bef45b2ce3d0ebf757e5979a8509574049c9882ca4d0feda21528b`;
`SOURCE-FENCE.json` hashes to
`49dcbf9ac19c7937046b4336ff70a7c66ddc7cab02764da74e73f5a1adf7862d`.

## Positive streams and causal matrix

Independent raw logs are under
`/home/by/urnetwork/temp/safe-current-proof-validation-aa9f715b`.
The final `manifest.json` hashes to
`5a44d2188c5300d7cb469035798bc3ffa62af690889b11e5d060853fec0e148e`.
Its 24-file `SHA256SUMS` hashes to
`6a52ca625c481c7965159e2ed9b1b950242dc19ee364a921717c0f9eae8fc5c5`;
every checksum passes. The independent read-only author audit of all raw
positives, controls, static/supplemental handoffs and current source/module/six
local-replacement fences is `/tmp/safe-current-storage-final-seal-audit.json`,
SHA-256 `885b26ad26ca0b08adfb309d053e2cc84c7170eedbc334ea8336989bb6f08182`.
The four completed
positive streams have exact slash-free top-level RUN/PASS census and package
PASS, with no failure, skip, panic, timeout or race report.

| Stream | Roots | Package PASS | SHA-256 |
| --- | --- | --- | --- |
| `light-normal.log` | 11/11 | 2.093s | `ad13f38a12f92cbafb67e37ad7f7263afa736d9d7abcc87c32db334d231c1045` |
| `light-race.log` | 11/11 | 20.558s | `66adc8e6bc5b5b002630c548806815be75a129c29f7bbc9ed259ec06649fe98e` |
| `adjacent-normal.log` | 10/10 | 5.801s | `9828626552cd514c6d9fa0599a150f4d0f4557a219da02298fbc35a5789f0f59` |
| `adjacent-race.log` | 10/10 | 49.502s | `dfbb3b7a6148d40a29e7972c159f18ce2c90af8ca0b80211b2b913a281df9ebe` |

The adjacent set covers retained canonical authority/provenance/orphan refusal,
published release/build/storage profiles and Safe digest/contract-signature
boundaries. It does not rerun the full eight-action graph, which is unchanged.
Harness environment is `GOMAXPROCS=2 GOPROXY=off`, `-p 1 -count=1 -v`, with
ten-minute normal and twenty-minute race package limits. Production read budgets
are unchanged. Exact selectors are in the sealed test plan.

| Control | Barrier | Normal | Race |
| --- | --- | --- | --- |
| `prefix_coverage` | Every intersecting proof branch is present. | Causal | Causal |
| `orphan_storage` | Unknown Safe storage cannot hide authority. | Causal | Causal |
| `header_binding` | Header hash and height match selected snapshot. | Causal | Not selected |
| `runtime_root_code` | Native runtime bytes match approved artifact. | Causal | Not selected |
| `published_code` | Proxy/singleton code matches published pin. | Causal | Not selected |
| `native_code_metadata` | Native code metadata agrees with code bytes. | Causal | Not selected |
| `policy_signature` | Distinct independent policy signature is required. | Causal | Causal |
| `canonical_snapshot` | Original selected hash remains canonical. | Causal | Causal |
| `pending_word` | Final scoped pending word has not changed. | Causal | Causal |
| `pending_completeness_claim` | Scoped recheck cannot claim complete pending proof. | Causal | Not selected |

Exactly five high-risk controls are selected for race, while all positive roots
run under race. Each selected control reached its named assertion, selected
root/package FAIL and process exit one, without build, panic, timeout or race
confounders. No result is claimed for the five unselected race mutations.
Integration preserves every Go/test/module byte and the SDK oracle of `aa9f715b`,
as well as the earlier immutable canonical/readmission/runtime receipts. Public
submission remains closed.
