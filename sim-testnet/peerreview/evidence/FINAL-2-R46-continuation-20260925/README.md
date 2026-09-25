# R46 retained continuation launch

R46 started at 2026-09-25 17:18:19 UTC as user service
`urnetwork-sim-release-r46.service` (initial PID 4123621). It uses the
unchanged signed plan `0x8bb92697db8f2164e46f6e58848d3407e509382fb61550b919f1d55391ad480e`,
the retained fleet supervisor, and LAN RPC `192.168.1.162:9944`.

The executable is SHA-256
`e8d2c017760e17bc9fb72c6b8d1802c18c7686c2c39a4f2b2ba75b32ff448cfa`,
built with clean Git revision `ad5c05eca00a73bada9fdc35dfa516487a616177`.
The launcher SHA-256 is
`2f71bf2e84ceb5caa0bb6ae4f14b22f798c6372b9f0a74a3799b1c6255945e34`.
It verifies its pinned inputs and the predecessor checkpoint before the
scenario CLI acquires ownership. `preflight-launch.json` is the actual
in-service check at 17:18:22 UTC. Its finalized LAN head was block 8,084,535.

The qualified source change allows a narrow set of authenticated process-log
findings to remain provisional while observations continue. It does not turn
those findings or the lifecycle companion exception into acceptance passes.
At 17:25:12 UTC the owner signed generation 46 with run ID
`20260925T172403.199659160Z-release-1.0`, after authenticating all 45 prior
generations. The copied signed envelope and its compact receipt bind the exact
R45 sealed failure and invalidation. They show `preparation_complete=false`
and no acceptance boundary. This bundle records launch and preparation, not a
measured R46 interval or final result. Those must come from later owner evidence.
