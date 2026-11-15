# Contract offline recovery and approval preparation follow-up

The [subsequent qualification receipt](bootstrap-contract-qualification-20260928.md)
records the nine-root normal/race pass, ten intended causal failures and
integration. The original author handoff below retains its earlier status.

This follow-up is based on `576dea586ba30e08fce6f3c7923b9717cc320b54` in
`/mnt/data/sn-testnet/worktrees/mg08-bootstrap-contracts-preview-20260928/sn`.
The preceding candidate and all its Terra captures remain frozen. The evidence
directory is `/mnt/data/sn-testnet/evidence/mainnet-bootstrap-contracts-preview-20260928`.

## Retained terminal status

Previously an offline command reopened a completed journal but always reported
`signed-custody-complete`. It now retains `reserve-created` or
`create-reverted-nonce-consumed`, including the unchanged original receipt and
attempt count. `receipt_observation` distinguishes `retained` from
`revalidated-online`; neither claims complete installation or activation.

`TestEvmCreateOfflineResumeRetainsCreatedReceipt` and
`TestEvmCreateOfflineResumeRetainsRevertedReceipt` exercise actual command,
private disk, local HTTP and reviewed geth EVM execution before reopening
offline. The latter uses a bounded low-gas original transaction to produce a
real failed CREATE with no installed code and its original nonce consumed.
Both compare receipt facts, original transaction, attempts, journal bytes and
all RPC counters after the offline reopen. The deterministic causal control
removes only the terminal-status projection; the intended assertion is
`offline reopen downgraded or replaced terminal custody`.

Author checks are compile-only and vet. Normal/race bodies and causal controls
belong to the separate Terra qualification lane and are pending. No live key,
signing, network write or deployment is included in this evidence.

## Unsigned approval preview

`bootstrap-contracts preview --config FILE` provides the missing executable
pre-approval path. It accepts an unsigned config with the existing schema and
public key, shares all structural/amount/route and exact artifact/constructor
validation, and exports the typed plan, hash and exact domain-separated signing
message. The run directory need not exist and is never inspected; a held or
malformed retained journal cannot obstruct public preview. No signing key,
signature import, journal owner or HTTP adapter is reachable from this command.
`plan/apply/resume` continue through full independent approval verification.

Six `TestEvmPhasePreview` roots cover exact bytes without custody, later
signature acceptance through the real command and existing owner, malformed
authority/artifact input, ambiguous JSON and execution flags, an already owned
opaque journal, and canceled/output-failed publication. The exact-message test
constructs the domain plus typed JSON independently. The signature test signs
only synthetic local data, admits the unchanged plan, and rejects unsigned,
changed-plan, wrong-key and wrong-signature resume without altering custody.

The complete affected-family selector is `^Test(EvmCreate|EvmPhasePreview)`;
the bounded follow-up runner selects the eight new roots and the failed
predecessor lost-reply root, without repeating the other 19 frozen passed roots.
Generator and native-root bodies are unchanged by this follow-up. Causal
controls remove offline terminal projection,
restore the circular signed-only preview gate, omit constructor equivalence,
or omit independent signature verification. A control qualifies only by
reaching its exact intended assertion; setup failures are retained failures.

## Lost acknowledgement assertion correction

The frozen corrected predecessor's only remaining EVM failure was a test's
search for the word `uncertain` in a diagnostic whose actual text says the
attempt may have reached the node. The follow-up keeps the required failing
command exit and proves one exact original HTTP write, reopens and validates
the retained original signed bytes/hash and attempt 1, then requires canonical
reconciliation of that same transaction with no additional write or attempt.
No submission, retry, approval or reconciliation policy changes.

Terra can rerun only this failed predecessor root and the eight new offline/
preview roots; the other 19 positive roots and all 12 intended prior causal
captures remain valid evidence for their frozen predecessor source. They are
not labeled as tests of the later source until an affected composed run occurs.
