# crv4 — Go-native Bittensor commit-reveal v4 (SP-2)

Go implementation of CRv4 weight commits for the UR subnet validator
(decision D-1: Go-native from day one, no Python sidecar). The validator
builds the weights payload, timelock-encrypts it **client-side** to a future
drand quicknet round, and submits a commit extrinsic signed by its sr25519
hotkey. The **chain** ingests drand pulses (pallet_drand), decrypts at the
reveal epoch and applies the weights — the client never sends a reveal
extrinsic. ("The chain handles encryption automatically" is false; it
handles *decryption*.)

Conformance harness binary: `sn/cmd/sp2`.

## Pinned upstream sources

| What | Source | Pin |
|---|---|---|
| subtensor runtime (pallet, extrinsics, chain-side decrypt) | https://github.com/opentensor/subtensor | release **v3.4.9-424** = `6d81084c5c13413d9e3637586280125c0bfc1948` (spec_version 424); cross-checked identical CRv4 files on main @ `14bc6f9f964b9cc362e9635dd110a487fa5d15a0` (spec 425). Public testnet ran **spec 423** on 2026-07-02; all facts below verified live against it. |
| reference client (payload, tlock, reveal-round schedule) | https://github.com/opentensor/bittensor-drand | tag **v2.0.0** = `0dff71eb4445c5b78c243be833754d811193830c` |
| tle timelock crate (ciphertext format; linked by BOTH subtensor's runtime and bittensor-drand) | https://github.com/ideal-lab5/timelock | rev **`5416406cfd32799e31e1795393d4916894de4468`** (pinned in both Cargo.tomls) |
| bittensor python SDK (call site, storage reads, u16 normalization) | https://github.com/opentensor/bittensor | main @ `c4dca6bbe3ae1ad7097ac2850558d8c602a5b44e` (post-v10.5.0; PLAN.md's "SDK v11 stateful epoch schedule" is bittensor-drand v2's `get_encrypted_commit_v2`, which this SDK rev uses) |
| go-substrate-rpc-client (gsrpc) | https://github.com/centrifuge/go-substrate-rpc-client | commit **`e3b938563803c6a71043c4e7ba2e0d27c400f514`** (dynamic-extrinsic support #389; NOT in tag v4.2.1) |
| drand quicknet chain info | https://api.drand.sh/52db9ba70e0cc0f6eaf7803dd07447a1f5477735fd3f661792ba94600c84e971/info | chain hash `52db9ba70e0cc0f6eaf7803dd07447a1f5477735fd3f661792ba94600c84e971`, genesis_time **1692803367**, period **3 s**, scheme `bls-unchained-g1-rfc9380`; group key pinned in `tlock.go` (matches bittensor-drand `src/constants.rs`) |

## The commit extrinsic (verified live on test.finney, spec 423)

`pallets/subtensor/src/macros/dispatches.rs` @ v3.4.9-424:

- **`SubtensorModule.commit_timelocked_weights`** — call_index **113**
  - `netuid: NetUid` (transparent u16), `commit: BoundedVec<u8, ConstU32<5000>>`
    (SCALE: compact-len + bytes), `reveal_round: u64`,
    `commit_reveal_version: u16`
- **`SubtensorModule.commit_timelocked_mechanism_weights`** — call_index **118**
  - same + `mecid: MechId` (transparent u8) after netuid. The SDK uses this
    variant with `mecid=0`; both route to `internal_commit_timelocked_weights`
    (113 uses `MechId::MAIN`).
- The legacy `commit_crv3_weights` (call_index 99) is **commented out of the
  runtime** — it no longer exists (verified absent live).
- `commit_reveal_version` must equal storage
  `SubtensorModule.CommitRevealWeightsVersion` or the commit is rejected
  (`IncorrectCommitRevealVersion`, `subnets/weights.rs:324`). Current value
  (default and live testnet): **4**.
- Signer = the **hotkey** (must be registered on the subnet). Commits are
  stored per `current_epoch_with_lookahead(netuid)`; ≤10 unrevealed commits
  per hotkey per epoch; rate-limited by `check_rate_limit` (one commit per
  epoch under default `weights_rate_limit`).
- `MAX_CRV3_COMMIT_SIZE_BYTES = 5000` (`lib.rs:60`). A 256-uid payload
  encrypts to **1313 bytes** — comfortable margin.

Signed extensions (runtime `TxExtension`, v3.4.9-424 `runtime/src/lib.rs:1640-1670`):
`CheckNonZeroSender, CheckSpecVersion, CheckTxVersion, CheckGenesis,
CheckMortality (custom wrapper, standard Era encoding+implicit), CheckNonce
(custom wrapper, standard Compact<u32>), CheckWeight,
ChargeTransactionPaymentWrapper (encodes exactly like
ChargeTransactionPayment: compact tip; spec 423 still has plain
ChargeTransactionPayment), SudoTransactionExtension (zero-size),
CheckShieldedTxValidity (zero-size), SubtensorTransactionExtension
(zero-size), DrandPriority (zero-size), CheckMetadataHash (mode byte;
we submit mode=Disabled, implicit None)`.
gsrpc rejects unknown extensions, so `chain.go` registers mutators for the
five subtensor-specific ones in `init()`; `CheckMetadata()` additionally
verifies any *future* unknown extension is zero-size before trusting the
builder.

## The payload (SCALE, golden-tested)

`pallets/subtensor/src/coinbase/reveal_commits.rs` (freeze_struct
`b6833b5029be4127`) == bittensor-drand `src/drand.rs`:

```rust
pub struct WeightsTlockPayload {
    pub hotkey: Vec<u8>,   // SCALE bytes of the committer AccountId32 = raw 32 bytes
    pub uids: Vec<u16>,
    pub values: Vec<u16>,
    pub version_key: u64,
}
```

parity-scale-codec encoding (`payload.go`): `compact(32) ++ hotkey ++
compact(n) ++ uids(u16 LE)... ++ compact(n) ++ values(u16 LE)... ++ u64 LE`.
At reveal the chain decodes and **rejects the payload if `hotkey` ≠ the
extrinsic signer** (`reveal_commits.rs:137-152`; a legacy hotkey-less layout
is grandfathered — we always send the new one). `version_key` must satisfy
the subnet's `WeightsVersionKey` check when the weights are applied.

Golden vectors in `golden_test.go` were generated with parity-scale-codec
via the Rust generator below.

## The ciphertext (tle format — NOT drand/tlock age format)

**SP-2 key finding:** the chain does *not* accept the Go
`github.com/drand/tlock` age-container format. Subtensor deserializes the
commit as an arkworks `CanonicalSerialize`-compressed
`TLECiphertext<TinyBLS381>` and decrypts with
`tle::tlock::tld::<TinyBLS381, AESGCMStreamCipherProvider>`
(`reveal_commits.rs:92-130`, tle rev `5416406`). `tlock.go` implements that
format directly on gnark-crypto:

```
TLECiphertext { header: IBECiphertext { u: G2 (96B zcash-compressed),
                                        v: Vec<u8> (32B), w: Vec<u8> (32B) },
                body: Vec<u8>,           // ark-serialized AESOutput
                cipher_suite: Vec<u8> }  // "AES_GCM_"
AESOutput     { ciphertext: Vec<u8>,     // AES-256-GCM ct || 16B tag
                nonce: Vec<u8> }         // 12B
// arkworks Vec<u8> = u64 LE length + raw bytes; total = len(payload) + 244
```

BF-IBE FullIdent on BLS12-381 (quicknet: sigs on G1, pubkeys on G2), from
tle `ibe/fullident.rs` + `curves/drand.rs` + `stream_ciphers.rs`:

- identity point `Q_id = HashToG1(sha256(round_be8), DST)` with
  `DST = "BLS_SIG_BLS12381G1_XMD:SHA-256_SSWU_RO_NUL_"` (tle `QUICKNET_CTX`
  == drand quicknet's basic-scheme DST, so real round signatures decrypt;
  w3f-bls `Message::new(b"", m)` hashes `ctx||m` = `m`)
- `sigma = sha256(t)[0..32]` (t = 32 random bytes), `esk` = random AES key
- `r = Fr::from_be_bytes_mod_order(sha256(sigma || esk))`
- `U = r·G2`, `g_id = e(Q_id, r·P_pub)`,
  `V = sigma ⊕ sha256(ark_bytes(g_id))`, `W = esk ⊕ sha256(sigma)`
- GT serialization: 12 × 48-byte **little-endian** Fp in arkworks tower
  order `c0.c0.c0 … c1.c2.c1` (gnark-crypto's tower is identical; only byte
  order per element flips) — `gtArkBytes` in `tlock.go`
- decryption (what the chain does): `sigma = V ⊕ sha256(ark(e(sig, U)))`,
  `esk = W ⊕ sha256(sigma)`, recompute `U` check, AES-GCM open

**Cross-implementation proof (offline `go test`):**
`TestDecryptRustReferenceCiphertext` decrypts a ciphertext generated by the
actual tle crate at the chain's rev (deterministic ChaCha20 rng) using the
**real quicknet round-1000 signature**
(`b44679b9…63ed5e39`, also pinned in tle's own test suite), and
`TestEncryptRoundTripRealSignature` shows Go-encrypted ciphertexts decrypt
under that real signature. Together these pin the container layout, point
encodings, DST, GT serialization and AES-GCM exactly to what the chain runs.

## Reveal round (stateful epoch schedule, v2)

bittensor-drand v2.0.0 replaced the legacy modulo formula. The SDK reads
five storage items at one block (`get_epoch_schedule_state`, verified names
in `lib.rs`): `LastEpochBlock`, `PendingEpochAt`, `SubnetEpochIndex`,
`Tempo`, `BlocksSinceLastStep` (+ head block number), then
`generate_commit_v2` simulates the chain's `block_step`:

1. extrinsic lands at `head+1` (`COMMIT_INCLUSION_BLOCK_OFFSET = 1`)
2. commit epoch = `SubnetEpochIndex` (+1 if the slot fires that block —
   `should_run_epoch`: `pending>0 && block>=pending`, or
   `blocks_since_last_step > MAX_TEMPO(50400)`, or
   `block-last_epoch_block >= tempo`)
3. reveal block = first block whose pre-`run_coinbase` epoch equals
   `commit_epoch + RevealPeriodEpochs` (exact equality)
4. `reveal_round = floor((now_f64 + (reveal_block + 3 − head) × block_time
   − 1692803367) / 3)`, min 1 (`SECURITY_BLOCK_OFFSET = 3`)

`schedule.go` ports this exactly; `schedule_test.go` pins it against the
reference vectors in bittensor-drand `epoch_schedule_vectors.rs` plus extra
vectors generated by running the reference `epoch_schedule.rs` verbatim in a
Rust harness (fixed `now` for full-round vectors; derivations in test
comments).

`block_time`: mainnet and the **public testnet** target 12 s (SDK default
12.0; measured ~12–15 s on test.finney 2026-07-02). Fast blocks (0.25 s)
are a localnet docker feature (SP-3).

## sr25519 hotkey

32-byte mini-secret seed file (hex or raw, `keys.go`), expanded
Ed25519-style via go-subkey/schnorrkel — same derivation as
subkey/polkadot-js/btcli, verified by the //Alice fixture
(`e5be9a50…5c0a` → `5GrwvaEF…utQY`, prefix 42). Extrinsic signatures use the
`"substrate"` signing context; payloads >256 bytes are blake2b-256
pre-hashed (gsrpc does both). ss58: prefix 42 for bittensor.

## Storage items read (pallet SubtensorModule)

`Tempo(netuid) u16`, `LastEpochBlock(netuid) u64`, `PendingEpochAt(netuid)
u64`, `SubnetEpochIndex(netuid) u64`, `BlocksSinceLastStep(netuid) u64`,
`RevealPeriodEpochs(netuid) u64` (default 1), `CommitRevealWeightsEnabled
(netuid) bool` (default true), `CommitRevealWeightsVersion u16` (default 4),
`MaxWeightsLimit(netuid) u16`, `WeightsVersionKey(netuid) u64`. Hashers come
from live metadata via gsrpc `CreateStorageKey`.

## u16 normalization

`normalize.go` mirrors the SDK: max-upscale (max→65535), `round(w×65535)`,
zeros filtered, negatives rejected (`convert_weights_and_uids_for_emit`);
plus `ApplyMaxWeightLimit` implementing the chain's `check_vec_max_limited`
constraint `max(w)/sum(w) ≤ max_weight_limit/65535` (self-weight and
limit=65535 exempt) via exact water-filling.

## Tests

- offline (`go test ./crv4/`): payload goldens, compact-codec edge cases,
  reveal-block/round vectors, Rust-reference ciphertext decrypt, real-
  signature round-trip, deterministic encrypt, structure/size bounds,
  malformed-input rejection, key fixtures, normalization.
- live gate (`SP2_LIVE=1 go test ./crv4/ -run TestLive -v`; optional
  `SP2_SUBSTRATE`, `SP2_NETUID`, `SP2_BLOCK_TIME`): metadata conformance
  (calls/args/extensions/storage), live `CommitRevealWeightsVersion == 4`,
  live epoch-schedule read + future-round sanity, full signed-extrinsic
  dry-run encode (never submits). **Run 2026-07-02 against
  wss://test.finney.opentensor.ai:443 (spec 423): all pass.**
- CLI: `sp2 check-metadata --substrate=wss://…` prints the same report;
  `sp2 commit … --dry-run` encodes a real commit without submitting.

## Golden-vector generator (reproducibility)

Rust crate with deps `tle @ 5416406`, `parity-scale-codec 3`, `w3f-bls
=0.1.3`, `ark-serialize 0.4`, `rand_chacha`: encodes the payload fixtures,
encrypts payload#1 to round 1000 with `ChaCha20Rng::seed_from_u64(0)` /
`esk=[2;32]`, self-checks decryption with the round-1000 signature, and runs
a verbatim copy of bittensor-drand's `epoch_schedule.rs` over the state
table in `schedule_test.go`. Outputs are embedded in `golden_test.go`.

## Remaining unverified items (feeds the SP-2 gate)

1. **End-to-end reveal on testnet**: an actual funded+registered hotkey
   committing via `sp2 commit` and the weights appearing in the metagraph
   after the reveal epoch (requires testnet TAO + UID registration; encode
   path is dry-run-verified, decrypt path is reference-verified offline).
2. **`burnedRegister` mirror semantics via the Neuron precompile** (the
   D-10 part of SP-2) — owned by the SP-1/precompile track; nothing here
   depends on it.
3. Runtime-upgrade drift: spec 424/425 rename the tip extension to
   `ChargeTransactionPaymentWrapper` (handled) — re-run
   `sp2 check-metadata` after each testnet runtime upgrade.
4. `pallet_drand` ingestion lag: `SECURITY_BLOCK_OFFSET = 3` blocks is the
   reference's cushion; if testnet pulse ingestion lags more, reveals retry
   every block within the reveal epoch (chain re-queues, `reveal_commits.rs`)
   — watch `TimelockedWeightsRevealed` events during (1).
