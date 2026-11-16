# Additive successor runtime authority qualification

**Scoped qualification complete.** All ten new and thirty-six adjacent roots
pass normally and under race detection: 92 positive root executions. All twelve
normal controls and exactly seven selected light race controls are causal.
The exact source/dependency evidence is sealed. This supplies no live submission
authority and does not implement automatic runtime compatibility.

Frozen source is `3d526830551ec2d8e7b0fc01f7bac5baa78e1789`, tree
`18a04980797221260b62c831fc7f5da3d026f5ab`, based exactly on qualified
`cd4261a8ae67cb326dd38f786cfeba03b26886ee`. The implementation worktree is
`/home/by/urnetwork/sn-successor-runtime-revisions-owner-20260930`.
The earlier canonical and readmission receipts remain unchanged and are evidence
for their own sources. This note does not inflate their counts into reruns.

## Behavior and limits

The [incremental authority path](../BOOTSTRAP-SUCCESSOR-RUNTIME-REVISIONS.md)
adds one separately signed runtime artifact at a time while retaining the base
canonical authorization, complete predecessor chain and original execution
custody. Hash-bound stages protect empty/partial imports. Counted and outcome
events name complete retained revisions without changing old v1 bytes, attempt
allowances, nonce claims, signed transactions, fees, windows or liabilities.

Current reads require exact version/code/metadata identity. Historical reads
select inclusion and parent from the complete independently approved history,
then authenticate both artifacts through the existing CRv4 checkpoint reader.
The full Safe fixture retains twelve revisions, consumes a reservation before
an unapproved upgrade refusal, sends exactly the original bytes once after
approval, and reconciles inclusion across two approved artifacts after a further
unapproved upgrade. The per-read pair avoids CRv4's ten-artifact allowlist cap.

This still requires independent approval for each new artifact. Automatic
compatible-change admission is a separate RT-04/P0. Same-version changed artifacts
and unsupported codecs remain closed. The genuine Safe deployment and complete
storage-history authenticator is still absent, and public `--submit` is rejected
before custody or network work. The local fixture uses explicitly synthetic
history authority; no live RPC, signing or transaction was performed.

## Sealed author handoff and independent evidence

Sol medium runs all behavioral tests and controls; Astra max implements,
debugs and independently audits. Author gofmt, compile-only and vet pass. All
twelve isolated production mutations also compile and pass vet, and the author
control worktree is restored clean. No author behavioral tests were run.

The 43-file handoff is `/tmp/successor-runtime-revisions-handoff-20260930`.
Its `SHA256SUMS` hashes to
`3ac788798a835a59780b009181c9f320e5250f3cef10dd10248924814cd09d69`;
`CONTROLS.json` hashes to
`91df4ef2b830c7123e23e1e030b0b59dcc4a7098376f06156e024512c86c406d`;
`TEST-PLAN.json` hashes to
`c1a8e2524c7d9dc16d5ae90f41fffa9f92da7ecae89c55a9733a88e7a01c7ab2`.
The source/module/six-local-replacement fence hashes to
`350397807dd5c5c20b73f5b6c42e91c107abea29614ae4af46a7f135e9f98db1`.

Independent logs are under
`/home/by/urnetwork/temp/runtime-revisions-validation-3d526830`.
The initial read-only raw audit is
`/tmp/successor-runtime-revisions-preliminary-positive-audit.json`, SHA-256
`2be160f9eb7544f8960c572246f4b9f6b53cfbd77df213aa6eb51e8b816390eb`.
The complete four-stream focused audit is
`/tmp/successor-runtime-revisions-focused-positive-audit.json`, SHA-256
`38e88c3e3255eacf5ce31129a6ec39fb62a8104f2e6becf2616a000e7523d516`.

The final independent `manifest.json` hashes to
`74876a9c8b78914a233c001779dc9309df16d8551ae4e7e52ead6299c839c17a`.
Its 44-file `SHA256SUMS` hashes to
`f9d6dda057b833490f954c7faac39b8dccd42e54986ee45340f04306c4f1d4c1`;
every checksum passes. The independent author audit of all raw positives,
controls and source/local dependency fences is
`/tmp/successor-runtime-revisions-final-seal-audit.json`, SHA-256
`dc230892b9afb2e7bae0aad20492e7052652bde024a83fe674231b43e7a5f2d5`.

| Stream | Roots | Result / package duration | SHA-256 |
| --- | --- | --- | --- |
| `light-normal.log` | 9/9 | PASS / 18.064s | `381a4a211e0834577a1e2f0a70e499d8f26b76b40c392f6fdf12e55aef280a56` |
| `light-race.log` | 9/9 | PASS / 78.102s | `be25a76a2dff1c3f4ac3870803e38004df43249bd402616f722cc8760d2a4dfb` |
| `command-normal.log` | 1/1 | PASS / 60.686s | `533f3e7f3c2a85bd6673d1e7c24c544a6aeffe92da7a8502d2aa9259a0ce9c86` |
| `command-race.log` | 1/1 | PASS / 426.430s | `5e7d51d354233df6e7fba51e315f54296792d6c6f8b3b3de7c0f996adc1653f5` |
| Adjacent normal/race | 36 per mode | PASS in both modes | Each raw stream is pinned in the final manifest |

The plan separates nine new light roots, one new heavy root, twenty-eight
adjacent custody light roots, three separate adjacent heavy roots, three original
receipt roots and two CRv4 checkpoint roots. Harness environment is
`GOMAXPROCS=2 GOPROXY=off`, `-p 1 -count=1 -v`; exact selectors and bounded
normal/race limits are in `TEST-PLAN.json`. Production read deadlines are unchanged.

The CRv4 normal command passed with two top-level roots and five nested cases.
An initial evidence parser incorrectly counted nested `RUN` lines as roots and
marked that result unqualified. Astra corrected the isolated runner to count
slash-free top-level names; no production/test source changed. Sol independently
reparsed the original normal and race PASS streams into the additive
`checkpoint-census-correction.json` (SHA-256
`3007e7526703082414f835166afca478ee7bf37cdd4dfc1dd834d6f9f2ec6afb`).
The original parser, false-failure ledger and unchanged raw logs remain sealed.
Normal package duration was 0.011s; race was 1.071s. No rerun was represented as
necessary to repair this harness-only census error.

## Causal matrix

All twelve mutations run normally. Exactly seven light mutations also run under
race; the heavy positive supplies normal/race behavior, while the heavy bypass
establishes its artifact barrier causally in normal mode.

| Control | Barrier | Normal | Race |
| --- | --- | --- | --- |
| `runtime_signature` | Original independent signer and domain. | Causal | Causal |
| `runtime_predecessor` | Exact signed predecessor. | Causal | Causal |
| `missing_runtime_reference` | Completed authority for a counted reference. | Causal | Causal |
| `partial_runtime_attempt` | No attempt under an incomplete revision. | Causal | Causal |
| `pending_terminal_revision` | Finish an exact terminal intent before import. | Causal | Causal |
| `runtime_history_snapshot` | Recheck retained history during ownership. | Causal | Causal |
| `runtime_code_identity` | Full current artifact code identity. | Causal | Causal |
| `retained_runtime_history` | Preserve more than ten reviewed profiles. | Causal | Not selected |
| `runtime_event_rollback` | Monotonic event authority. | Causal | Not selected |
| `legacy_optional_field` | Preserve old event bytes and seals. | Causal | Not selected |
| `historical_parent_authority` | Independently approve the parent artifact. | Causal | Not selected |
| `historical_pair_authentication` | Authenticate exact inclusion/parent artifacts. | Causal | Not selected |

Each selected control reached its exact named assertion and root/package FAIL
with process exit one, without build failure, panic, timeout or race-report
confounders. The normal heavy historical bypass is causal; its complete positive
fixture also passes under race. No result is claimed for the five unselected
race mutations. Integration preserves every Go/test/module byte of `3d526830`
and the earlier immutable canonical/readmission qualification receipts.
