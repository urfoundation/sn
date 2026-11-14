# Paired restore assertion correction

Frozen `39e2744ae639711ab43c104d227cbfc2c213df33` produced five passing and two failing roots in Sol's seven-root normal run. The original log remains at `/mnt/data/sn-testnet/sol-outer-rebind-39e-independent-20261003/evidence/seven-normal.jsonl`; it is not relabeled as a pass.

Both corrections change tests only. Production and module files remain identical to `39e`.

- The public pending-outcome control omitted the original reserved `contract-successor-execution-002.intent` and incorrectly allowed a suffix-free final filename. It now decodes and hashes the original pending payload, verifies its exact staged inode after offline adoption, and requires that same inode and payload at the intent name after canonical completion. The final `.json` must contain those exact bytes. Sequence, predecessor, approval, runtime, policy, receipt and cumulative liability remain checked; every other original member and nonce remains unchanged. The two exact event names are the only additional outcome members allowed.
- The passive work control incorrectly expected a full payload read after acknowledged immutable metadata changed. Confirmed metadata loss must refuse with zero new payload reads and remain sticky after restoration. A separate pending-stage control uses an explicitly retained unacknowledged reservation, counts its actual partial/full reads, proves unchanged checks read zero additional bytes, and verifies that the passive owner neither acknowledges nor publishes it.

The fixed controls still require independent behavioral qualification. This source correction does not close capacity revisions, current-module composition, production restore approval or activation.
