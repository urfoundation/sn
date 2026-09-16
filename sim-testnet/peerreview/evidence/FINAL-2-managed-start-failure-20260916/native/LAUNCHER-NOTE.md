# Launcher evidence correction

Root invoked the prepared wrapper as `bash strict-resume/command.sh`.
The wrapper then changed to the frozen source checkout, where its relative
`$0` could not be resolved by the input-hash capture. The original
`command.inputs.sha256` therefore contains the REQUEST hash but lacks the
command-script hash. The exact tool-observed warning is preserved in
`launcher.stderr.observed`.

The wrapper does not stop on that capture failure. Its native resume command
started at 2026-09-16T05:47:36Z and remains owned by root session 97810,
outer PID 3219802 and CLI PID 3219921. No native operand, source, binary or
approval changed, and the process was not restarted. The unchanged script
and REQUEST are hashed explicitly by their absolute paths in
`command.inputs.supplement.sha256`; this is a later supplemental observation,
not a replacement for the original capture.

Use the absolute prepared command path for release-candidate and all later
wrapper invocations. This is a launcher invocation correction and does not
require a source patch or repeated qualification.
