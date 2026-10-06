# Production runtime history at upload admission

The standard validator's upload signer retains its existing wire contract.
For chain 964, the destination upload cache now requires
`reserved_attempt_upload.admission.production_runtime_config`: a
`ReleaseEvidenceV2File` with the exact `path`, `bytes` and `sha256` of an
independently provisioned, signed schema-3 validator configuration. A current
runtime tuple alone cannot authenticate the original activation after an
upgrade, and cannot establish the current runtime's approval expiry.

The server already embeds `ValidatorUploadAdmissionConfig` in its strict YAML
configuration and passes it to `NewValidatorUploadAdmission`. No endpoint,
upload header or requesting validator chooses this reference. Provision it
through the server's approved configuration. Its file must be a private regular
file in an owner-private directory, with the referenced approval and production
history documents available at the paths selected by the signed configuration.
Resolve those paths before approving and copying the configuration; changing
them changes the signed configuration. Custody paths remain strings: the upload
loader never opens a native seed, starts a producer or submits a transaction.

The loader checks the exact config bytes, the separate approval signature and
the finite production history, including exact original full-authority bundles
selected by a renewed config. It matches chain, genesis, subnet, coordinator,
settlement vault, deployment identity and current runtime against the server's
independent deployment. The upload census bound cannot exceed the signed bound.
It then discards the complete configuration and its production signing capsule,
retaining only immutable read-only deployment, route and runtime-window values.
Later reads do not reopen the mutable input paths. A new process independently
loads the pinned files again.
The runtime-only projection combines those original windows with explicit runtime
documents and rejects conflicting overlaps before discarding the full authority.
It does not retain original economic configs or any prepared submission grant.

Every mainnet refresh checks the native route against the signed route list and
reads its chain name, genesis and EVM chain identifier. Activation authentication
selects the one exact runtime approved at the activation's original native
height. Current timestamp and permit observations select only the signed current
window. Earlier windows, compiled catalog entries, numerical version ordering
and unchanged artifact bytes outside the approved interval grant no current
authority. Both historical and current registration, non-self stake and permit
still come from the actual native readers. The config's validator census is not
an upload allowlist: other independently anchored, currently eligible validators
remain discoverable.

The cache's existing freshness and request limits still apply. A failed refresh
invalidates the cache and cancels its leases; an individual invalid activation
does not acquire an entry. Block-window expiry is enforced at each observation,
not continuously between refreshes. Requests make no RPC calls and retain the
existing bounded cache/lease freshness. Original activation consent does not
replace current eligibility, account authentication, object binding or quota
checks.

Local regressions join the ordinary upload signer, real server-used admission
constructor/refresh/lease, signed synthetic schema-3 config, real native storage
decoding and a local EVM HTTP fixture. Three controls restore the former decisions:
identity-only history rejects the approved production upload; a current-tuple
observer accepts a height beyond its signed interval; an ordinary optional struct
tag adds an absent authority field to legacy JSON configuration bytes. An absent
pin remains omitted, while an explicit pin survives serialization. The corrected
paths also cover expiry, current permit loss, altered config pins and scope, changed network
identity, unapproved historical artifacts and cancellation.
The [authority-history qualification](evidence/production-authority-history-qualification-20260928.md)
adds an actual signed upload whose original activation depends on a renewal
bundle rather than a duplicate runtime document. Both historical and current
eligibility are read, and the detached projection survives source-file deletion.

This closes downstream runtime-history propagation. It does not prove a live
mainnet deployment, trusted RPC service, approval custody/revocation, historical
archive availability, remote upload delivery or service activation. Upload
admission grants bounded staging capacity; it does not authenticate the eventual
measurement, migration prefix, settlement outcome or a weight transaction.

[Retained qualification](evidence/upload-runtime-qualification-20260927.md)
records the exact normal/race binaries, three causal controls and consumer builds.
