# Runtime458 capacity causal — closed

The forward capacity mutant changes only `crv4/runtime_identity.go`, reducing the reviewed per-chain metadata artifact bound from six to five. It was applied only in a detached disposable ade worktree with a recorded 12-dependency projection.

Two normal binaries were compiled with the existing offline four-CPU contract. The two CRv4 roots produced one expected failure (`complete six-identity history failed`) and one pass. The two simulator roots produced one expected failure (`complete release history did not reach its exact artifact reader`) and one pass. Both bodies had binary exit 1 because each contains its declared negative control; both event conversions and exact outcome checkers exited 0.

The candidate source and all 13 candidate projection rows remained unchanged before and after; the mutant diff was unchanged and reverse-apply checks passed. No network or native action occurred.
