# Provider request custody

An operator retains its original provider requests only when its signed release
config sets `request_preparation` and `request_receipt_scope`. Without them the
validator opens no request journal and publishes no request windows.
`sn-mainnet bootstrap-chain validator-source-role-config` exports both fields
unsigned from a pinned `ProviderAttemptRequestPreparation`. They take effect
only after owner signing and adoption, and after a stopped-writer
`storage-prepare` whose attempt-ledger owner carries a `requests` scope with
that same preparation.

## On disk

The operator's ledger root holds `provider-attempt-requests.jsonl`, at most one
`provider-attempt-request.pending`, and the checkpoint attribute
`user.urnetwork.validator.requests.v1`. The attribute is the commit point. Each
pending reference, committed head and sealed window is one atomic attribute
replacement, followed by a directory sync and an exact readback. Recovery reads
it to resume, clean up or refuse an interrupted append.

The attribute shares the root inode with the assignment custody checkpoint,
durablevolume's storage-preparation anchor and its root generation. ext4 without
`ea_inode` keeps all of them in the inode's spare bytes and one 4 KiB block.
This release stores the checkpoint as one fixed 599-byte record with schema
`urnetwork-provider-attempt-request-custody-v2`. The record holds the SHA-256 of
the original preparation, the directory and journal inodes, the committed head,
the pending reference, and the signed closed window without its copy of the
preparation. Identity, limits and birth come from the separately retained
preparation, and a digest mismatch refuses the record. Storage preparation
writes the record at full width, so a later replacement never needs more space.
`Validate()` refuses a preparation unless one ext4 block holds all four
attributes at their widest. It counts the block header, the 16-byte entries,
the names and values padded to four bytes, and the terminator. A fresh native
journal, SDK work outbox or successor-members owner may share the root and
attach its own attribute there, outside that count. So storage-prepare and the
runtime refuse a request root that carries any other `user.urnetwork.`
attribute. Snapshot owners keep theirs on their own lock files and may share.

Earlier releases wrote the whole checkpoint as JSON (v1). Once a window closed,
it grew to about 2.9-3.4 KB, more than ext4 leaves for it beside the root's
other attributes, so the first `SealWindow` failed with ENOSPC on every retry.
This release still reads v1. The runtime, offline inspection and restore accept
it, and restore rebinds its copies exactly as before. The runtime's next save
replaces it with v2 in one atomic write, and restore writes v2.

## Downgrade

An older release reads only v1. It refuses a v2 record and fails closed: it
does not open the request journal and writes nothing. This release writes v2
whenever it writes the checkpoint: when storage-prepare creates or restores the
root, and when the runtime appends a request, seals a window or finishes an
interrupted append. So a validator with `request_preparation` enabled must not
be downgraded below this release once this release has written its checkpoint,
including by its own storage-prepare. Sealing a window is only one such write.
Only a root whose v1 record this release has read but not yet replaced can go
back.
