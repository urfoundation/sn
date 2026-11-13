# Local restore signed-input fixture correction

Independent `bdf63dea` reached the complete-union check in all four public
local-restoration roots and refused unassigned original members. The separate
test-only diagnostic `e266603e` identified exactly eight `*.signed.bin` files
and no missing owner attributes. Its receipt is
`8937e0c81ed79778a41d8a91fc2fce1a78ce434052cc1f14e9270051de811e02`.
These original failed scopes remain unchanged.

The files came from `evmCreateFixture.signSelectedAction`, which had placed
external inputs into `config.Plan.RunDirectory` after a composed fixture moved
that runtime root. Production does not emit these names. `evmCreateOwner.advance`
authenticates imported signed bytes against the selected action and retains
them in `evmActionRecord.Signed` before any send. `validateForAction` rechecks
the envelope, transaction hash, config, predecessor and allowance; the owned
submission adapter sends that retained record. Public resume without an input
argument consumes this journal. `BOOTSTRAP-CONTRACTS.md` already places exported
signed input files outside its example runtime directory.

This test-only correction keeps exported inputs beside the original fixture
config before they are imported. It does not delete or relocate files during
restore, rewrite a signature, exempt unknown paths or widen a production bound.
All eight originals remain there. The common public restore helper proves
byte equality with every authenticated action journal, proves complete disjoint
coverage of every exported runtime file and checkpoint, then performs the real
public plan/apply. Afterward it checks that every input file is unchanged.
The four settled/reserved/acknowledged public roots share this preflight.

A cheap placement regression forces the separate config/runtime layout using
the same fixture signer. On the old helper it creates the unowned sidecar. On
the correction the runtime root stays empty and the exported transaction bytes,
digest and approved envelope remain exact. Existing single-action fixtures
keep the same path because their config and runtime directories coincide.

The adjacent source audit keeps other custody owners distinct: native raw SCALE
files are actual `chain.Journal` members with their own fixed preparation and
restore census; successor Safe signatures and signed relayer bytes are embedded
in the immutable execution approval and checked against their pinned external
inputs. Their ownership is not inferred from this file suffix. An operator who
places unrelated inputs in a runtime root must retain and review them; the
complete-union planner continues to refuse an unassigned file.

Behavioral qualification of this successor is pending Sol execution. This
does not qualify outer pending-head adoption, retained capacity changes, the
latest published module composition, a release, or any live action.
