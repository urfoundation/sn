# R47 generation-2 staging diagnostic repair

The activated, authenticated generation-2 handoff was selected while both operator APIs still loaded generation-1 activation contexts. Both validators repeatedly received HTTP 403 for initial terminal publication. `PREVIEW.json` binds the exact handoff and four replacement context references per operator; `APPLIED.json` records the local replacement and byte-exact backups retained outside the repository; `PROOF.json` records the API-only restart and the first healthy validator recovery. The original runtime manifest was not updated. This is a provisional diagnostic exception and cannot establish final acceptance.

`ROOT-MISSED-648.json` contains the two finalized LAN-RPC transaction receipts at block 8,090,227. Both vault RootMissed event amounts are zero. The old and new `st.yml` bytes contain private deployment material and are retained only under the restricted qualification directory; this review bundle contains hashes and public on-chain receipts.

`R47-ACCEPTANCE-BOUNDARY.json` freezes the signed attempt bytes at the 12:53 UTC boundary and its five-epoch window. The live attempt may append later observations, so its later file hash is expected to change.
