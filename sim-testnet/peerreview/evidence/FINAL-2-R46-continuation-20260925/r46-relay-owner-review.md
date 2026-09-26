# R46 historical relay ownership investigation

The historical owner lookup is correctly refusing absent evidence. The retained v3 diagnostic contains 66 validator-2 publication members. All 64 closed-census members from epochs 604–635 lack a request, receipt, and action owner in the diagnostic's retained journal. Both later-audit members have existing requests whose headers match the captured signed artifacts. This includes missing custody for all ten closed-census slots in the acceptance interval 631–635.

The first rejected slot is `c142b961b93bd182e1e67bcd12bd0b8e87cb6fa68381ed3c78f485928763ec07`, validator-2/operator-1/closed-census/epoch-604. `readFinalRelayCaptureRequest` fails while opening its request, before historical plan decoding or chain readback. There is no predecessor plan record that could supply the missing request or its original admission.

The source of the gap is the closed-census scheduler's single cursor across independent generations. In the retained predecessor namespace, validator-2 has closure/publication 602 but neither closure nor publication 603. Retained policy-gap records cover 594–602. The authenticated handoff declares successor cutoff 604, first full epoch 605, and no ledger continuity claim. The original scheduler waits for predecessor publication 603 before selecting the successor namespace. The successor's separately initialized cursor is unused. This control flow explains the complete absence of validator-2 successor closed-census custody. Audit discovery independently inventories its subjects, explaining why both captured audit members have requests.

Validator-1 provides an adjacent control: its predecessor closure/publication 603 and corresponding gap records exist, and its successor has relay requests beginning at 604. The runtime and capture source files are identical between diagnostic driver `363882d6` and investigation base `902b9c56`.

All retained-source inspection was read-only and used zero RPC calls. The original diagnostic, live state, shared branch, and sealed result were not changed. File paths, hashes, all 66 slot identities, and journal absence checks are in `retained-relay-owner.receipt.json`; `inspect-retained-relay.py` reproduces that census.

## Future scheduler correction

The isolated change schedules each installed generation using its own cursor. Missing predecessor 603 remains unresolved. The independently authenticated successor starts at its signed cutoff; it cannot skip a missing epoch within its own generation, read a predecessor file at/after cutoff, or replay an already processed successor slot through the predecessor cursor. Every source still passes the same complete public census, signatures, original budget admission, and transaction owner checks. Completion retains the maximum verified epoch so a delayed predecessor cannot regress completed successor progress.

No changes were made to historical ownership lookup, final capture, the handoff authenticator, policy-gap proofs, or acceptance range gates. This fix cannot manufacture the original R46 custody and does not turn the retained diagnostic into a pass.

## Qualification

The four new deterministic tests use the real successor installer with signed activation and finalized chain readback, real dual-signed public census bytes at both local test replicas, and the production scheduler. They stop at the read-only admission gate to verify routing without inventing transactions. Tests cover an interrupted predecessor, an independently missing successor cutoff, a foreign predecessor owner in the successor namespace, cutoff replay, and strict refusal of an absent historical request.

The unmodified scheduler fails the successor-read, foreign-owner-inspection, and cutoff-replay tests. An overlay retains the exact base scheduler while running the final corrected test source. Focused normal/race qualification also includes existing policy-gap, handoff, owner-cache, missing/foreign/overdrawn request, predecessor custody, range wait, and final-capture checks. Exact inputs and the reviewable patch are recorded in `qualification-inputs.json` and `generation-scheduler.patch`; final execution results are recorded separately in `qualification-results.json`.

Qualified commit: `c83498b49a69162ea1a6f3dda53094e4edab9ad4`. Final-source pre-fix red: three intended failures. Corrected focused normal: 29/29 pass (13.830 s). Corrected focused race: 29/29 pass (94.921 s), no race reports. The worktree is clean.
