# Production runtime admission qualification

Base: `32b6a19c91e5275bc44a6407ecedf1a8b58e1e16`.
Worktree: `/mnt/data/sn-testnet/worktrees/sn-mainnet-runtime-producer-20260927`.
Date: 2026-09-27 UTC.

This record qualifies the schema-3 config/runtime portion of the coherent
owner-recycle producer change. Its approval, source/intent and archive pieces
are qualified separately in the same commit. Fixtures use synthetic identities,
generated fixture keys, scripted native RPC and local HTTP servers. No live RPC,
signature, submission, installation or activation was performed.

## Checks

The following completed successfully:

```sh
go test ./crv4 ./validator -run '^Test(ValidatorProducerRuntime|ProductionRuntime|ReleaseActivationV2|ReleaseBootstrapV2|ReleaseEvidenceV2HistoricalRuntime|ReleaseEvidenceV2Decision|ReleaseCaptureV2|ReleaseNativeCapture|ReleaseStartupV2)' -count=1
# crv4 2.520s; validator 42.430s

go test -race ./crv4 ./validator -run '^Test(ValidatorProducerRuntime|ProductionRuntime|ReleaseActivationV2|ReleaseBootstrapV2|ReleaseEvidenceV2HistoricalRuntime|ReleaseEvidenceV2Decision|ReleaseCaptureV2|ReleaseNativeCapture|ReleaseStartupV2|MainnetRuntime|AuthenticatePinnedNativeRuntime|LoadReleaseConfig|ReleaseConfig|ReleaseNativeValidator|ReleaseProvisionalRuntime|ValidatorUploadProvisional|ReleaseRuntime|ReleaseCurrentRuntime|OwnerRecycleAdmissionConfig)' -count=1
# crv4 23.629s; validator 369.143s

go test ./validator -run '^TestProductionRuntimeCapture' -count=1
# validator 7.577s; includes the later production-capture extension

go test -race ./validator -run '^TestProductionRuntimeCapture' -count=1
# validator 28.913s

go vet ./crv4 ./validator
git diff --check
```

The new controls exercise the default production config loader; exact independent
version/code/metadata pins; old/current finite block intervals; schema and purpose
refusal; immutable loaded config; metadata, API and signing-extension drift;
foreign or fabricated artifact proofs; read-only signing refusal; cancellation
with joined completion; closing canonical-hash changes; and old evidence surviving
failed replacement reads. Historical startup and activation use actual native
SCALE storage/stake/API decoders, dual-key consent and EVM HTTP readers. They
reject missing config authority, changed permit and changed epoch.

The capture control creates a real provider measurement, encrypted prepared source
batch and signed sidecar. Its decision is at native block 101 while the approved
drain is at block 100. A cold capture retains activation metadata, LastUpdate and
PendingServerEmission at their distinct original blocks. A deliberate capture
storage failure remains the returned error and does not change the caller's view.

## Exact old-source controls

Go overlays restore one complete file from the base commit while retaining the
new regression and its synthetic fixture. Neither control is a compilation failure.

| Restored file | Regression | Normal | Race |
| --- | --- | --- | --- |
| `validator/runtime_identity.go` | `TestProductionRuntimeConfigAuthenticatesExactCurrentArtifact` | Fails: independently approved config rejected by the compiled 461-only gate | Same failure, 1.686s |
| `validator/provisional_runtime_compatibility.go` | `TestProductionRuntimeSigningRejectsReadOnlyExactArtifact` | Fails: read-only artifact acquired signing authority | Same failure, 2.876s |

The commands use `go test [-race] -overlay <overlay.json> ./validator -run
'^<regression>$' -count=1`. Each overlay maps the absolute worktree source path to
`git show 32b6a19c:<file>` output. Old-source SHA-256 values:

- runtime identity: `c0e7f3a28cee1bf6d540d2cfdafbb659d7a8616130ddbbaf7901602f66b00df6`
- signing/runtime compatibility: `7b675d48f0b7c3850617cf36efaf6d6aec518edf2f303ad9cfb7aeb817e0cfc8`

Raw control logs, overlay JSON and the runtime file manifest are retained at
`/mnt/data/sn-testnet/qualification/production-runtime-20260927`. The sorted
22-file SHA-256 manifest has digest
`e87a4e806c623dea6d7298288e34813d59b0fbb54c085f1e5200734e58bf8af1`.

This is local code qualification. It does not supply a reviewed live runtime,
independent production approval, deployed companion contracts, bootstrap authority,
native economic outcome or original schema-3 approval/config migration history.
