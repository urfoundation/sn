# Validator unsealed-inventory qualification, October 1

The read-only `admit-committed` observation now retains a separate bounded
inventory of each validator service UID's actual unsealed ledger bytes. Frozen
implementation `f8dba5b1` and test-only successor `ffe0d339` were integrated
without changing the original committed-checkpoint authority or enabling
public start. The inventory authenticates original nonlegacy import receipts,
signed ledger ancestry, unfinished trail counts and hashes, and protected
service-owned sources. It preserves prior unsealed checkpoints across later
observations and retains a canonically empty intent boundary when present.

The [author receipt](/mnt/data/sn-testnet/validator-unsealed-observation-20260930/evidence/author-result.json)
records 17 new roots passing normal/race across the two freezes, 31 adjacent
normal roots, vet, actual foreign-UID custody, and five intended causal failures.
Its manifest SHA256 is
`795a308a6dca2d2236d13fcaa8ca57c9f6f6d4f6418404b2161c6e94dd0f7c0e`.
The [independent Sol receipt](/mnt/data/sn-testnet/validator-unsealed-sol-qualification-20261001/evidence/qualification-receipt.json)
is sealed with SHA256
`11585e353c534cda38c953ac90a7e62d87c0f85d7454e02816d7027bfb289a76`.
Its selected normal and race suites each passed 46 ordinary roots; three
additional UID-only roots passed under actual root privileges in both modes.
Vet passed. Seven independent causal overlays failed their intended assertions,
including signed-tail, prior-prefix, cross-owner-close, intent and custody
guards.

This closes a bounded local-liability inventory subgate only. Nonempty intent
graphs and full historical chain binding of unsealed tails remain unverified;
those states refuse this bounded mode rather than being treated as empty or
already accepted. Current recovery images, per-operator live worker evidence,
global signer custody, applied weights influence and signed launch authority
also remain open. No live service, signer or chain action was performed.
