# Explicit successor registry rebind profile

This source increment adds a separately approved physical registry adoption to
the existing public successor execution commands. It does not qualify a live
restore, approve a signature, submit a transaction, or authorize service start.
Behavioral qualification belongs to the separate source-pinned Sol receipt.

`bootstrap-chain contract-successor-execution-rebind-preview` uses all original
execution inputs, approval/hash and accepted execution hash, plus
`--registry-restore-plan` and `--registry-restore-plan-sha256`. The explicit
`--durable-volumes` / `--durable-volumes-sha256` declaration must contain the
restored registry generation and every unchanged local owner needed by the
original command. The single-root preparation output is not substituted for a
deployment declaration.

The preview emits the exact domain-separated signing bytes and a plan binding
the original execution approval, old physical registry, completed restore plan,
original inventory and former-writer assertion, both member-census hashes, and
the new physical generation/declaration. Only the independently pinned original
approval key may sign `urnetwork-mainnet-successor-registry-rebind-v1`. The
`urnetwork-mainnet-successor-registry-rebind-envelope-v1` envelope contains
`schema`, that exact `plan`, and `signature_ed25519`. The command implements no
signer or key generation.

`contract-successor-execution-resume` and
`contract-successor-execution-readback` accept the envelope through
`--registry-rebind-approval` and `--registry-rebind-approval-sha256`. Fresh claim
rejects these flags. Original signatures, nonce payloads, execution hash,
cumulative counts and fee allowance remain in their original domains. All
reviewed restored nonce members must survive exactly; completing an original
pending nonce retains its payload and any acknowledged inode. Ordinary online,
canonical, current-policy and submission capability gates still apply.

The first profile requires already completed local claim/nonce acquisition and
an unchanged original local preparation root. It retains one immutable rebind
receipt in that local execution history only after original custody checks.
Loss or replacement of an acknowledged receipt is an integrity failure. A joined
retry may finish the exact retained rebind publication, but an unrelated pending
local publication is refused rather than overwritten. This first profile does
not support rebind chaining, adoption during a pending original local outcome,
or automatic declaration/capacity renewal.

Complete-union co-owned restore, local-root physical rebind, pending outer
member-census recovery, in-place retained preparation, capacity revisions and
the current published SN/server/Connect composition remain required work. The
storage result continues to report `restart_authorized: false`; independent
approval and actual host/device authority remain separate inputs.
