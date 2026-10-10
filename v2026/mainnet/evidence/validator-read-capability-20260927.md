# MG-04 / RT-01, RT-02: validator read capabilities

Date: 2026-09-27. Source baseline: `c16eddea3a05b1d94910dcc47b47eaf0dc8cabf2`.
Toolchain: Go 1.26.6, linux/amd64. Qualification uses synthetic exact approvals
and scripted block-pinned Rpc responses. No live chain, campaign state,
release lock, owner-trim source or NetEscrow source was changed.

## Root cause and boundary

`crv4.ReadValidatorStakeAtContext` authenticated a caller-approved exact
runtime/code/metadata tuple, then required its spec version to be one of seven
compiled values. A newer artifact needed testnet provisional authority to
pass, even if independently approved and compatible. The failure affected
shared stake/permit observations used by validator startup, activation,
registration inspection and historical schedule readers.

The shared reader now checks a read capability before its first storage
decode. Exact approved successors need the same storage prefixes, hashers,
key/value types, optional/default behavior and fallback bytes as the reviewed
baseline for the eight consumed storage items. Metadata portable type numbers,
unrelated pallets, calls, events and signed extensions are not read authority.

The selective-metagraph API must declare version 2 at the same block. Its
runtime identity must still match the authenticated artifact. The complete
77-field response decoder, census bounds, canonical/finalized block checks,
stake threshold comparison and registered-owner exception remain unchanged.
The reader reauthenticates the exact artifact after identity observation and
never rebinds the shared chain's metadata or signing runtime.

Adjacent caller review found that `ReadValidatorScheduleAtContext` first
reads a reverse UID mapping and later consumes `SubnetEpochIndex`. It now
requires the schedule capability before its first storage read. That capability
adds epoch-storage compatibility. A changed epoch interface suspends schedule
observation while a stake-only observation can still use its compatible subset.

Historical adapters preserve their existing exact caller authority. A genuine
connection-owned provisional proof retains its already checked profile. The
new strict successor path does not install provisional authority or authorize
any transaction. Release configuration gates, source/build provenance and
production admission remain separate and unchanged.

## Deterministic tests

`crv4/validator_stake_runtime_test.go` drives the real public stake and schedule
readers with exact fixture-approved artifacts outside the compiled version
list. It checks:

- Successful future-spec stake and schedule reads at the selected historical
  block, with unchanged shared metadata/runtime and no provisional authority.
- Admission despite removal of unrelated calls, events, pallets and signing
  extensions; rejection of changed consumed widths, hashers, defaults,
  modifiers and prefixes before the first storage decode.
- Missing, changed, duplicate or malformed consumed API versions, and malformed
  metagraph response bytes despite a compatible declared API version.
- Exact version/code/metadata authority, mixed identities forced at both
  reauthentication boundaries, and cancellation inside capability observation.
- A changed epoch layout refusing schedule reads before storage while the
  same artifact still supports a stake-only read.

These transitions use explicit scripted boundaries, without sleeps or live
endpoints. Fixtures own all mutable metadata and transcripts; parallel tests
exercise the shared immutable baseline and real metadata cache.

Negative control: restoring only baseline `crv4/validator_stake.go` through
a Go source overlay makes `TestValidatorStakeCapabilityAcceptsApprovedFutureSpec`
fail with `validator stake runtime layout has not been reviewed` (0.489s).
The same test passes with this patch. No checkout or live file was replaced
for the negative control.

## Reproduction and results

Run from the repository root:

```sh
validator_read_tests='^Test(ValidatorStakeCapability|RuntimeArtifactMetadataValidator|ProvisionalRuntimeCompatibility|FleetProvisionalRuntime|FleetMainnetRuntime|FleetMainnetAuthority|FleetMainnetReceiptRuntime|ReleaseProvisionalRuntime|AuthenticatePinnedNativeRuntimeValidatorStake|ReleaseEvidenceV2HistoricalRuntime|ReleaseCurrentRuntime|ReleaseRuntime[0-9])'
go test ./crv4 ./miner ./validator -run "$validator_read_tests" -count=1 -timeout=180s
go test -race ./crv4 ./miner ./validator -run "$validator_read_tests" -count=1 -timeout=240s
go vet ./crv4 ./miner ./validator
git diff --check
```

Normal: PASS — CRv4 6.160s, miner 5.493s, validator 28.048s.
Race: PASS — CRv4 51.742s, miner 46.930s, validator 158.145s.
Vet and whitespace checks passed.

Local logs and negative-control overlay:
`/mnt/data/sn-testnet/evidence/validator-read-capability-20260927/`.

## Limits

This is read capability qualification, not automatic production admission.
The caller still supplies independently approved exact artifact identities;
the endpoint cannot create that approval. Runtime family, transaction and
state encoding versions remain bounded. API declarations and wire-shape
agreement do not prove unchanged economic semantics of arbitrary new Wasm;
source/build approval and ongoing semantic postconditions remain necessary.
The current release configuration still selects reviewed runtime identities,
and CRv4 signing, other native operations, production upgrade admission and
composed controlled-upgrade evidence remain open under MG-04 / RT-01 through
RT-08.
