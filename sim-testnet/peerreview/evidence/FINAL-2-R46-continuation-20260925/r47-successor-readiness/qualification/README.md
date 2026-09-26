# Authenticated generation2 successor handoff

Qualified isolated commit `73305340fee0e5114989fcb548c0414ee1650d69`, based on `1b4751be15a79a5dc905b854f2a5dcc337767792`. Worktree clean. No push, live-state edit, transaction, stop, restart, staging or activation was performed.

The original writer tried to replace immutable `policy-rollover/handoff.json`; deterministic pre-fix red in `pre-fix-red.log` reports `compact input journal already names different immutable bytes` (exact reproduction source retained). The final regression activates a real second signed four-member generation, retries idempotently, and proves the original handoff and sealed input bytes remain unchanged.

A same-policy successor now approves the exact previous handoff reference plus advancing generation/cutoff. An immutable versioned postcondition is durably written before the fsynced verified activation journal row selects it. The original locator stays untouched; no mutable active pointer was added. Missing, duplicate, wrong-parent or torn committed selectors fail closed. Both supervisor and validator process records must be stopped for activation. Legacy first-activation crash recovery may finish only its exact original locator.

Worker configs, upload contexts, observations and relay sources consume the authenticated chain. The relay routes original/gen1/gen2 namespaces at their cutoffs and retains the original historical policy-gap waiver and receipt identity. Latest cutoff plus one remains the earliest complete acceptance epoch. Retained runtime verification keeps the original 31-file generation inventory while authenticating successor configs, references and custody separately. Native hotkeys accept the existing hex/raw formats; client keys stay raw-only.

| Qualification | Outcome |
| --- | --- |
| Deterministic pre-fix fixed-path regression | Expected FAIL, exit 1 |
| Core 11 tests, normal | PASS, 56.780s package time |
| Core 11 tests, race | PASS, 133.232s package time |
| Added generation2 source-role interaction, normal / race | PASS, 9.151s / 23.990s package time |
| Two existing genuine native-slot controls, normal / race | PASS, 2.526s / 20.397s package time |
| Final diff / source hashes | Clean; production matches both core snapshots, final source matches both source-role snapshots |

Exact commands and source hashes are in the four `qualified-*` / `source-role-*` JSON receipts. All use the pinned R45 modfile and command-local umask 0077. The narrow selector now also matches the later-added source-role test; the original core logs record the actual eleven test names executed at their source snapshots.

Superseded failures are retained, not claimed as passing qualification. Broad normal finished with the intermediate native hotkey loader defect and missing epoch10 RPC fixture; broad race was explicitly interrupted after that fixture failure because it also selected unrelated costly fleet-renewal tests. The intermediate `final-*` logs failed those same issues. The production native loader bug was corrected specifically to `LoadSeedFile`, with actual-reader hex/raw controls and raw-only client rejection; the RPC fixture was corrected separately. `superseded-broad-stop.json` records the exact test child signaled. The live supervisor was untouched.

The adjacent generation-native-role path is separately tested: generation2 selects gen1 V2's exact predecessor; V1 remains nil when empty; native mismatch prevents overlay publication. Simulator descriptor cryptography is an explicit synthetic boundary, while the two validator tests exercise real signed historical proof and native-slot verification. These tests do not replace live native evidence.

The exact proposed post-handoff plan/review/apply sequence is `/mnt/data/sn-testnet/qualification/r47-launch-readiness-20260926/commands-successor.md`. After a complete current allowance/plan review, stage while live, officially stop, activate the exact generation2 handoff, prepare/review/apply its distinct `--rollover-source-role` plan, then retained resume with zero setup actions. V1 missing intent plus an occupied native slot is a hard blocker; V2 needs its exact applied/finalized gen1 proof to match the current slot. Fresh generation2 measurements and the new release assertions still determine acceptance. The blocked SN25 merge is excluded.
