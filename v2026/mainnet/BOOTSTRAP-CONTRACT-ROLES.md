# Offline contract-to-validator declaration admission

`bootstrap-chain contract-role-plan` binds two independently signed UR producer
configs to the exact approved contract graph. Earlier v3 inspection checks that
the two configs agree with each other; agreement alone permits both to name a
foreign deployment or the coordinator implementation instead of its proxy.

```sh
sn-mainnet bootstrap-chain contract-role-plan --config /private/chain.json
```

The command rereads the original v3 input and every transitive signed config. It
reconstructs all eight approved action projections through `evidence-create`
using the pinned release artifacts. Each producer's coordinator must equal the
predicted proxy address, its settlement vault must equal the approved vault, and
its policy identifier must equal the exact atomic proxy initializer's policy
hash. Both configs may be individually valid and mutually consistent while this
separate admission refuses their shared foreign declaration.

The sealed result binds the original preparation and contract-plan hashes,
artifact pin, predicted implementation/proxy/vault/evidence addresses, the
evidence journal's complete immutable deployment domain, and each role's exact
config pin and signed approval hash. `declarations_verified` means these precise
offline relationships passed. It does not establish live policy parameters,
actual CREATE heights, the evidence anchor or an executed installation.

`declared_deploy_block` remains an unverified scan-floor declaration. Canonical
receipts must later prove that it cannot omit required contract history. Current
contract runtime/getters, the anchored evidence journal, operator evidence,
current roles and both validator service activations require separate admission.
Every original pending phase remains; canonical receipt, current state,
installation, activation and network-effect fields remain false.

This command has no custody, signature-import, online, submission or service-start
port. Exit zero confirms declaration admission only; invalid scope/input returns
two and output failure returns one. It does not change any v1/v2/v3 preparation
hash or recovery journal. The new result is not accepted as production service
authority; future activation must consume independently revalidated installation
and role evidence. Public Safe current-policy submission remains closed pending
the separate policy approval and qualified release route.

The corrected `f4470d0d` [qualification receipt](evidence/bootstrap-contract-role-qualification-20260930.md)
records eight focused and twelve adjacent roots passing normally and under race
detection, plus all three causal controls in both modes. The original `43dcd01f`
fixture setup failure remains separate evidence. Astra max owns implementation,
debugging, fixes, formatting, compile-only checks and vet; Sol medium owns
behavioral qualification. No live RPC, real signer or mainnet action is part of
this increment.
