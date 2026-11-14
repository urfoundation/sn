# Pinned public Safe release inputs

These are unchanged upstream public release archives consumed by the offline
verifier, not deployment configuration or a selected user's Safe identity.
They retain their upstream license notices and sources; no package scripts run.

| Package | Archive SHA-256 | Source commit / tree |
| --- | --- | --- |
| [@safe-global/safe-contracts 1.4.1](https://registry.npmjs.org/@safe-global%2fsafe-contracts/1.4.1) | `8803f7cf26e2c58300b8136342faa7b8bce59436e300f52115fa25b095e0bf79` | `bf943f80fec5ac647159d26161446ac5d716a294` / `dbbe8faa94445342975303ff4da1471cac2052d6` |
| [@safe-global/safe-smart-account 1.5.0](https://registry.npmjs.org/@safe-global%2fsafe-smart-account/1.5.0) | `6ffc8ccbd8c5de2a9909bd072a4bb677b4d84b25f126757bdec9f26ef732fc29` | `dc437e8fba8b4805d76bcbd1c668c9fd3d1e83be` / `b994261bb400a74b58133f65beab522a5fdd25ee` |

The catalog `profiles.json` has SHA-256
`a3bb95c9234181a5e3017a130655cfbf6cd53f64c5f19108b9a1555362cbf626`,
independently pinned in the verifier source. Its exact artifact/build-info/ABI/
bytecode/layout hashes were captured from those archives. Both published
production builds identify `solc 0.7.6+commit.7338295f`; all 42 Safe-owned input
files in 1.4.1 and all 66 in 1.5.0 match the retained release tags and packaged
source bytes. See the pinned [1.4.1 source](https://github.com/safe-fndn/safe-smart-account/tree/bf943f80fec5ac647159d26161446ac5d716a294)
and [1.5.0 source](https://github.com/safe-fndn/safe-smart-account/tree/dc437e8fba8b4805d76bcbd1c668c9fd3d1e83be).

The published production build-info also contains test/support compilation units.
Four external inputs in 1.4.1 and twenty-one in 1.5.0 match the locked
OpenZeppelin contracts 3.4.2 archive, SHA-256
`72f4176e8d4db8825a6161a7663061f3c3d3f2f6e14c7d5357bec6bd1dd9c7bd`.
One further 1.5.0 input matches Safe mock-contract 4.1.0, archive SHA-256
`46d559557b77894db7107c1c7647a818e4e88610789b6074441555b2bdddac95`.
Their archives matched the upstream lockfiles' SHA-512 integrity; individual
source hashes are recorded separately in the catalog. See the pinned
[1.4.1 lockfile](https://github.com/safe-fndn/safe-smart-account/blob/bf943f80fec5ac647159d26161446ac5d716a294/yarn.lock)
and [1.5.0 lockfile](https://github.com/safe-fndn/safe-smart-account/blob/dc437e8fba8b4805d76bcbd1c668c9fd3d1e83be/package-lock.json).
No support contract is selected as the verified singleton or proxy.

The retained generation helper and public dependency evidence are under
`/home/by/urnetwork/temp/mg08-safe-release-profile-verifier-20260929/evidence`.
The companion dependency design is
`/home/by/urnetwork/temp/mg08-safe-dependency-design-20260929/SAFE-DEPENDENCIES.md`.
Generation reads archives and compares sources; it does not install npm packages,
execute third-party scripts, compile Solidity, query a chain or sign anything.
An independent compiler rebuild and release/audit review remain open.
