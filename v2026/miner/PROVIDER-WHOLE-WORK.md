# Provider whole-work capture

The standalone `provide` / `auth-provide` commands and embedded provider swarm
configure the real SDK contract manager through a reviewed capture profile.
The profile supplies an independent window-request public key, the complete
client-key policy domain, an existing provider client ID and public key, and an
explicitly prepared private outbox. The retained provider seed must match that
public key.
Capture mode refuses new client allocation; it never generates a replacement key
to satisfy a profile.

Pass the exact original profile and its independently reviewed digest:

```text
provider provide --whole-work-capture=/absolute/original/capture.json \
  --whole-work-capture-sha256=sha256:<reviewed-64-lowercase-hex-digits> \
  --require-whole-work-capture
```

An absent optional profile retains legacy providing and unknown whole-work
evidence. An explicitly required, missing or invalid profile refuses startup
before authentication or wallet setup in the CLI. Complete-profile startup
failure exits nonzero after owned workers have joined. An explicit profile also
requires `--allow-client-registration` to be absent. Existing custody may use the
separate original-key adoption procedure documented in
[PROVIDER-REGISTRATION.md](PROVIDER-REGISTRATION.md).

The JSON schema is `urnetwork-provider-whole-work-capture-v1`. Its fields are:

| Field | Meaning |
| --- | --- |
| `api_url` | Exact HTTPS operator origin used by the provider. |
| `request_public_key` | Independently reviewed 32-byte Ed25519 request authority. |
| `providers` | Complete launch census, between one and 64 entries. |
| `providers[].slot` | `direct`, the actual proxy slot, or the exact swarm member ID. |
| `providers[].client_id` | Existing 16-byte provider client identity. |
| `providers[].public_key` | Public key derived from the retained provider seed. |
| `providers[].domain` | Complete original `ClientKeyHistoryDomain` object. |
| `providers[].outbox_directory` | Canonical absolute, prepared, process-owned `0700` directory. |

Byte arrays use JSON arrays of integers. Every slot and client ID must be unique;
outboxes cannot overlap or share physical custody. Request signing authority
must differ from the provider signing key. Unknown or duplicate fields, changed
profile bytes, symlinked profile files, missing directories and unsupported
lifetime-lock platforms are refused. The reader creates no profile, key, identity
or outbox. The optional close-report domain must agree when it is available.

Before the first SDK launch, accepted offline `storage-prepare` must publish the
outbox's empty birth checkpoint and original index under its stopped-writer and
zero-history fence. The SDK outbox preparation owner uses kind
`sdk-original-work-outbox`, the exact approved capture-profile reference and its
provider slot. Restored custody instead comes from the complete protected
original inventory and retained signed bytes. Making an empty directory or
copying selected cut files cannot authorize startup. The shared production
provider constructor validates prepared custody and signed scope through Core
before creating the SDK device. Capture and replay retain that same approved
provider key in their runtime settings. The live worker subsequently acquires
and monitors its own outbox lease; a later custody change refuses capture.

Swarm members use `whole_work_capture`, `whole_work_capture_sha256`, and
`require_whole_work_capture`. A top-level swarm `require_whole_work_capture: true`
requires a complete profile for every member, including after disable/restart.
Each member profile has one owner whose slot is that member's ID. The actual
capture HTTP pool receives the same provider dial and TLS trust settings and is
joined through the SDK lifecycle.

The SDK signs an enrollment for its actual client, key, domain and newly created
manager generation, posts it to `/provider-work/v1/owners`, and polls
`/provider-work/v1/requests`. Enrollment proves possession of that key only. An
independent authority must still admit the complete expected generation roster
and sign the exact start/end window requests. A public receipt or SQL row cannot
supply that authority. There is no existing bootstrap or artifact signer that
implicitly approves this new request key.

For each admitted request, the SDK retains original signed cut bytes before
posting to `/provider-work/v1/cuts`. It holds one outbox lease for the lifecycle.
On restart, retained cuts are replayed exactly; the new manager enrolls a fresh
generation and cannot manufacture the old generation's missing end cut. Never
delete originals, replace the outbox with an empty directory, or reuse a prior
generation to make an incomplete window appear complete. Capacity, missing
requests, missing owners and incomplete cuts remain unknown evidence.

`bootstrap-chain provider-role-config` accepts `require_whole_work_capture: true`
on its host request and an explicit `whole_work_capture_profile` reference
(`path`, `sha256`) on every host. It reads that independently reviewed profile,
checks the original operator/domain and one direct-provider owner, and exports
the exact miner arguments above. It does not create or sign a profile and does
not reuse the original bootstrap approval as request-key authority. Legacy
exports report `whole_work_capture_configured: false`; successful configuration
still leaves `activation_ready: false`. Complete roster/window verification,
Server request/cut custody, actual deployment and financial conformance remain
separate admission gates.

The source requires Core `77069204`, including `ValidateOriginalWorkOutbox`,
`BuildFreshOriginalWorkOutboxCheckpoint` and the explicit runtime `PublicKey`,
plus SDK `9ae95704`. The corresponding SN `storage-prepare` adapter must also be
included in the final source composition.
New tests cover public CLI refusal, public bootstrap export, and the shared
production DeviceLocal constructor's actual HTTPS capture, cancellation and
restart. These are distinct paths, not one successful public-CLI end-to-end
capture. They are authored but unexecuted at this handoff; previous miner scope
qualification does not qualify this new feature.

Offline preparation and recovery use
`DecodeProviderWorkCaptureProfile(ctx, raw, expectedSha256)` after their own
protected profile read. It verifies the exact reviewed bytes, independent
authority, complete roster, domains and canonical nonoverlapping paths without
opening the named outboxes. Recovery must select the exact approved slot and
match its outbox to the original protected inventory. The decoder grants no live
custody: launch still uses `ReadProviderWorkCaptureProfile` and its physical
directory checks. Custody admission also requires every retained request's
provider key to match the exact approved profile. Multiple generations under
that key can restart; history under a rotated key requires a separately approved
original profile and key-history gate. A retained leaf cannot authorize a key
change, and a later approved launch key cannot rewrite those cuts.
The three portable decoder test roots are separate, authored and unexecuted;
decoder commit `ad703e8d` preserved the original eight miner and three bootstrap
test sources. The prepared-custody successor adds three constructor refusal roots
and updates the two lifecycle roots' shared fixture to explicitly prepare its
synthetic birth before its first SDK launch. These revised and added roots are
also authored and unexecuted.
