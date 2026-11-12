# Release ABI artifacts

These files are generated from the reviewed Foundry output by
`stabi/generate.sh`. Release 1.0 callers use `STCoordinator`,
`STSettlementVault`, and `STReserveSink`; `STSubnet` is retained only for
legacy migration/read compatibility.

`STSettlement2.abi.json` is the STSettlement2 proxy's ABI, generated from its
newest implementation, `STSettlement2Ctf`, which keeps every phase A and phase
C entry. `STCtfCarry.abi.json` is the capture the flag carry contract's.
