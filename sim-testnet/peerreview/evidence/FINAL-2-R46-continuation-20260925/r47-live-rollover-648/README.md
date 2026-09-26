# R47 generation-2 consent publication, 2026-09-26

The clean `550bbaa6` driver (SHA-256
`4c2d4e8dd01fe0ef6be991a037ead37e42e9db94ed983395e097f792981521a5`)
created immutable no-apply plan
`0xaecc96a8238d6309bb3d611627194a198390f3b458b4f9e830cacdada99fa120`
for generation 2, activation epoch 648. The archived `plan.json` and printed
JSON are semantically equal. `live-budget-review.receipt.json` records the
independent full-liability check before apply.

The exact-plan apply published four zero-value consent transactions on the
real testnet using LAN RPC `192.168.1.162:9944`. The independent
`four-consents-portable.receipt.json` verifies each successful transaction
receipt against a canonical finalized block, exact sender/target/nonce and
the matching journal postcondition:

| Member | Block | Transaction |
| --- | ---: | --- |
| V1 / NO1 | 8,089,581 | `0x2521447bd5209affe148f561e38923a9df1ac7208738a512159dd899e3903129` |
| V1 / NO2 | 8,089,584 | `0xd8600af6028eaaa84f2ad07bae5cc1e618d05c1a913f6dad186c50ef895f0965` |
| V2 / NO1 | 8,089,587 | `0x6f7eb0be1ffd0cdb0cbf7a3a945b26587bfb44ee9d234d759046848d8d13aead` |
| V2 / NO2 | 8,089,590 | `0x022f374cae5c062cf1764b15b23735da284d3aabbb1f7de14011b8f0e0ea8c9c` |

The stage command exited 1 with its explicit expected boundary result:
publications are complete; resume the **same plan** after block 8,089,774
finalizes. Generation 1 remained selected and the retained supervisor was
not stopped. This bundle does not claim generation-2 activation, a fresh
native ledger, a new release interval or final acceptance.
