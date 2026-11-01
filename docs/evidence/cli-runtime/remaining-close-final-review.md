# Remaining Close Fixture Review

Status: passed for the three C fixtures, the Go verifier, and focused public tests.
No blocking finding remains.

The HTTP fixture selects its client descriptor at `connect`.
Its receive hook cancels only that descriptor.
The close hook closes that descriptor before it reports one injected error.
Other descriptors use the real close function.

The File fixture separates three release points.
Case 7 selects the local removal descriptor at finalization.
Case 8 skips that descriptor and selects the retained observation directory during scope cleanup.
Case 10 selects a retained grant directory after a successful write.
Each selected close reports one error after actual release.
Case 8 requires diagnostic evidence for `close observed directory`.

The server fixture uses a dedicated child directory as its working directory and write grant.
The executable remains in the parent directory.
The hook selects one matching owner-thread descriptor after callback shutdown.
This selection excludes callback-thread release and Console image ancestors.
The client still requires connection closure without a successful response.
The public test keeps its primary failure, completed write, single cleanup entry, and resource assertions.

Private writer invocations can delegate close and shutdown before fixture initialization.
The hooks resolve those real functions when their stored pointers are absent.

The original broad File hook hid the difference between local finalization and retained scope cleanup.
The corrected scope case preserves its primary failure even when the Lo error handler returns a value.
The primary finalization case reports `runtime.host-failure` with `input/output error` and confirmed removal progress.
Its structured verifier requires no recoverable error, secondary cleanup entry, or completed activation.
It also requires four confirmed bytes and failed index minus one.
The existing count and stage checks remain unchanged.

All eight public groups passed on Darwin software in 7.775 seconds and Linux QEMU in 21.951 seconds.
Each target passed nineteen cases.
The runner reported no timeout and no remaining owned process.
The runtime source and bundle remain unchanged.
The full stack CI remains pending.

Evidence and reviewed byte digests appear in `remaining-close-final-review.json`.
