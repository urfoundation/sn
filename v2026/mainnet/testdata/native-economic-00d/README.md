These are synthetic literal JSON fixtures in the checkpoint/policy layout of
SN `00d7efcd18c3a4d08c2883de9c50bb6376cba716`. They were constructed from that
revision's field order, with no runtime catalog, runtime labels, capacity
revision, or read-budget revision fields. They are not captured production
state or a claim that an old executable ran in this qualification.

The checkpoint keeps one original incentive event and a nonzero cursor. Its
fixed content hash is
`sha256:98c3e5bb3021d099f0180c28d397d5ccb087d00450ffc7d2caf6d63aa0787db7`.
The tests require the new loader to preserve its checksum, exact serialization,
and history without rewriting the file on open. A separate public-command test
substitutes fixed-size synthetic block hashes and the original policy hash,
publishes that old wire record through the real guarded owner, and resumes the
original cursor with an explicitly increased read budget.
