# Literal slash test names: source candidate

The retained Connect qualification has source-declared child names such as
`rtt_500ms/preferred_true/legacy_false` passed to one `t.Run` call. Go emits the
root and complete child identity; it does not emit a test for each slash part.
The earlier qualification parser required the immediate slash prefix to be a
declared test, so it refused these otherwise complete event streams.

The version 1 declaration format now uses the longest exact declared slash
prefix as the parent. Names remain literal, case-sensitive, bounded to the
existing 4096 bytes and 64 slash parts, and tied to a declared top-level root.
No expected identity or parent is learned from the observed events. Unknown
events still fail, every declared ancestor must be live, every declared child
must terminate before its parent, and failed ancestors and descendants retain
their separate declared assertion literals and original binary exit.

A separately declared prefix remains a parent constraint. An ambiguous flat
sibling whose name extends another declared sibling is therefore refused when
the prefix is no longer live; this change does not infer a different tree from
runtime ordering. Reviewers must derive the complete declaration set from the
test source, including genuinely nested parents.

Four new deterministic roots generate actual Go test events in joined, bounded
child processes and check admission, read-only replay, real nested and parallel
children, unknown or misordered parents, original failure attribution, finite
names and shard ownership. The only updated legacy expectation replaces a
literal slash child incorrectly classified as an orphan with an actual
undeclared-root orphan. The production event verifier is unchanged.

Qualification was pending at this source seal. The subsequent
[qualification and retained-event replay](qualification-slash-parent-qualification-20260928.md)
passed. The original proposed scope follows: Terra will run the affected
checker roots in normal and race modes and controls that restore the immediate
prefix rule or skip genuine declared ancestors. Only after those pass should
the retained Connect events be replayed with the independently reviewed full
declaration census. Their original execution, capture failures, source fences
and binary exits remain evidence; replay does not rerun Connect or create new
source/execution qualification.
