# R48 isolated native recovery qualification

This bundle qualifies the offline one-edge implementation. No live file, process,
validator state, signer or approved plan was changed. The original proposal and
native-pool history bundle remain byte-for-byte unchanged.

`qualification.json` pins the modified Go source bytes, toolchain, selectors and
raw log hashes. Three deterministic RED receipts reproduce an unapproved child
argument, callback retargeting of the approved epoch, and the early-sealed/source
and no-generation/no-prelock-write boundaries. The final focused validator and
simulator suites pass both normally and with the race detector. Sanitized logs
omit environment, operational paths and temporary fixture names.

The early-failure test reuses R47's observed/planned blocks 8091300/8092324,
settlement653, six assertions/five failures, active/pending faults and cleanup
after the last observation. Identities and owner signatures remain synthetic.
The original R47 signature/result byte hashes are in the proposal. Recovery
preserves the failed partial interval and requires current restoration separately.

`current-schedule.json` is a read-only LAN observation using the exact recovery
schedule reader, selected 467/1/1 anchors and consumed-interface profile. Both
validators authenticate actual runtime471/1/1 at finalized8091767/native1694;
retained intent/EMA epoch1690 and all source digests remain unchanged. This is
not exclusive capture or authority to choose a future first epoch.

`current-pool-audit.json` authenticates the current settlement654/source653
commitments, public signed artifacts and exact deposits at finalized8091801.
Both pools satisfy those prerequisites for uncontrolled validator2. This does
not prove fresh statistics/quality, actual positive pool weights, subsequent
native emissions, vault stake growth or nonzero capture. Those remain distinct
observation gates. Public wallet bytes are projected to hashes; raw observation
hashes are retained. No credential, rendered config or operational URL is copied.

The two `.go.txt` collectors may be copied to separate temporary `.go` files and
run through the qualified workspace before cutover. The schedule collector takes
`--state-root` and the approved `--substrate` endpoint. The pool audit collector
takes the selected validator2 `--config` and approved `--evm` endpoint. Both are
read-only, time bounded diagnostics; neither is a recovery request or proof of
final acceptance. Recheck the facts when choosing a future native epoch.

This isolated patch does not include the separate signed 60/60 harness migration
or its native integration seam. Its final integration must authenticate the real
archived old config and immediate predecessor, and bind the separate migration
receipt. Strict historical-runtime and final archive authority remain separate.

Verify these portable files with `sha256sum -c SHA256SUMS` in this directory.
