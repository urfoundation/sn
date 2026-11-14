# Qualified durable owner composition

The exact SN `69f4bbdd68f3ba933fc0e6b8994708664689acbc`, tree
`4e49dc8fba437bb55939eaea09ef190fde89c8e4`, and server
`10a8f4d8ab73822b4c796f9035486f0507e09bd2`, tree
`622c52b73f71b7652cf508856198ffa6aa85f574`, pass their bounded composition gate.
This is an incremental source qualification, not a new production release,
mainnet deployment approval or closure of the whole hardening tracker.

The [author receipt](durable-owner-composition-author-20261002.json), SHA-256
`3b4fc3a9d09b582ecd3a953a0769d450bcf43b3da92f215e208481c156710c5b`,
records 78 selected normal and 78 race roots, no failures or skips, and two
repository vet commands. Seven disjoint groups cover mainnet 21, observer 17,
successor 13, validator 8, miner 5, inspection 2 and server local blob 12 roots
in each mode. Root rehashed all 382 file bindings and the Go tool binding,
checked exact clean source pins and the unique per-mode root census.

The [independent receipt](durable-owner-composition-independent-20261002.json),
SHA-256 `fc6bd88f6313411ab602f4e568d2393160b0281a143c4cf7e52abc6741d1f4af`,
passes twelve public/role controls per mode: mainnet eight, miner one, validator
one, inspection one and server one. Affected-package vet passes. Root verified
all 37 entries in its separate manifest. Its physical source copy verifies
20,018 SN and 5,240 server Git blobs. It does not independently rerun all 78
roots or turn older component scopes into a new full-suite result.

The source join SHA-256
`63238579bc1844c44bc2fdfdd210f24aebf85739b4b02041867e3b612cdf655f`
binds 279 changed SN and eight changed server files to exact retained component
blobs and physical bytes. Earlier native, snapshot, miner, monitor and adopter
receipts retain their own scopes. The composition includes corrected observer
`6d398662`, exclusive writer `f3c8a618` and test-only diagnostic `33e57ee7`.
Original failed fixture, observer and writer attempts remain retained.

The effective SN module graph selects Connect `0a5cda0e`; standalone server
selects Connect `6cd720cf`. The later `71df099c` constructor fix is not inherited.
Incoming server `6387a012` code outside the eight local-blob files is preserved,
not broadly qualified. Origin server `2bc74cf0` was fetched after this seal and
is a separate later intake. Neither newer pair nor module successor can reuse
this receipt as if its source were identical.

Provider startup/callback isolation and progress monitoring, immutable-member
recovery, executable offline preparation, runtime continuation, capacity/restore
and the remaining [38-requirement backlog](mainnet-implementation-backlog-20261002.md)
remain open. No signing, broadcasting, service launch or restart authority is
provided by these receipts. Raw logs, tools and physical sources remain in the
retained author and independent workspaces named by the receipts.
