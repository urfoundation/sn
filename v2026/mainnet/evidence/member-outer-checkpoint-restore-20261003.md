# Interrupted member checkpoint restoration

This candidate extends the fixed local and nonce member restore adapters to
retain an interrupted outer snapshot checkpoint. Earlier source refused every
`Checkpoint.Pending` at planning, even when both exact completed metadata images
were present in the authenticated physical export.

The preparation core now permits one unsigned physical census and one optional
`companion_path`, each at the unchanged maximum of 8 MiB. The fixed adapter
selects those names from the original checkpoint. Ordinary signed members never
gain rewrite authority. Both original files, hashes and staging generations
remain separately retained in the plan, and the same pure inode-only derivation
is verified again before apply. Neither a repeated derivation nor an omitted
companion can be interpreted as complete coverage. Absent companion fields keep
the earlier single-metadata plan encoding and interpretation.

The member adapter uses the existing snapshot restore grammar to prove the
before-exchange or after-exchange state. It validates both complete census
images against every original signed member and pending payload. Only inode
fields in those unsigned images and their physical checkpoint are rebound.
The preparation result retains `restart_authorized: false` and preserves the
pending outer checkpoint. The actual exclusive member constructor then performs
its existing one-attempt reconciliation after prior owners join. It does not
make a new claim, discard history or replace an original pending payload.

Application member count, per-member bytes, aggregate member bytes, namespace
entries, both bounded census images and total preparation limits are distinct.
The 4,096-member/256-MiB application limits are unchanged. The extra metadata
image is explicitly bounded and included in the request's aggregate file/byte
admission. No funded slot, gas allowance, signer, chain approval or retention
horizon is inferred from physical capacity.

The source controls target public preparation followed by the actual local and
nonce member writers at three real publication boundaries, plus refusal of a
newly hash-pinned but incomplete pending image. Core controls cover both exact
originals, lost or altered lineage, and joined recovery after either reviewed
metadata inode move. Source compilation is not behavioral qualification.

Higher-level independent registry/local approval readers still have their own
single-census lineage and passive-pending constraints; their interrupted-head
adoption must be composed explicitly. This does not close capacity revision,
retained in-place migration, chained rebind, current published-module composition
or live restore acceptance. No service, transaction, device or remote database
is exercised by preparation.
