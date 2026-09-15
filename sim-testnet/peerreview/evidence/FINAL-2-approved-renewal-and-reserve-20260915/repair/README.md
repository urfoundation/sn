# Finalized probe deployment and 6,000-alpha reserve repair

All observations use `http://192.168.1.162:9944`; `independent_rpc=false`. The capture performs read-only RPC calls and contains no pending signed plan payloads.

The repair extrinsic `0xb6468a8c03886ef4d3c207ba08348b3219267b90d7e569b82d9a81b9da96ebed` appears at index6 of the seven extrinsics in native block8,009,634, hash `0x201b281662dceed0baf5b2d1efae66373867ffa252565417b8c7fb1d4380dbc3`. BLAKE2b-256 over the exact SCALE extrinsic bytes reproduces that hash. The canonical block header links to parent8,009,633, hash `0x796fe5b2cf7239b51a55ea66f8d4e9566811298f698be696731d2fdbe3799cb9`.

Pinned `TotalHotkeyAlpha` storage reads at those two native blocks establish a source debit of exactly6,000,000,000,000rao and reserve credit of exactly6,000,000,000,000rao, preserving their combined total. `SUMMARY.json` contains both public hotkeys and all four amounts. The simulator's separate finalized postcondition also confirms the destination coldkey stake increased from52,995,154,815,981 to58,995,154,815,981rao. The per-coldkey stake and aggregate hotkey stake are distinct quantities.

Probe transaction `0xa53f90989089c770f09624e6ef700a0d5760594aec889a082f499462dbdd68c0` has an EVM receipt with status1 at block8,009,630, EVM hash `0x2ebb45543f40ea3b1decb043949d5571b3c5039745513dce91b6b8556fbcb42c`, contract `0x500c8955e0b76848f1f32c0e67c7e0715c1ecf00`, and gas used1,511,354. Its EVM hash must not be substituted for the native block hash. This is deployment evidence; the campaign's precompile conformance remains to run.
