# HTTP Error Input Fixture Review

Status: passed for the two selected files and their focused public tests.
No blocking finding remains.

The Linux fixture now enables its guest loopback interface before it starts the service.
The socket and ioctl sequence matches the existing `http-lifetime.c` fixture.
The sequence preserves the interface flags and adds `IFF_UP`.
It closes the control socket before it starts the service thread.
The Darwin branch remains unchanged.

The Go test uses one helper for both target wrappers.
Both wrappers keep the same ten input cases, independent interpreter expectations, and output assertions.
Each case stops the service with SIGINT and joins its thread before the second CLI invocation.
The fixture still checks the retained error, callback count, and final resource counts.

The original Linux run failed all ten cases at fixture status 14 before the service connection.
The corrected maintained groups passed in 21.361 seconds.
Darwin passed ten cases, and Linux passed the same ten cases through QEMU.
The reviewed runtime source digest remains `b3eb0376362bafe13e6c5e7f2609100544f6f355bcb699b36f0a593051b4670d`.
This fixture correction does not change runtime source or its bundle.
The full stack CI remains pending.

Evidence: `cli-cancellation-linux.log`, `cli-cancellation-both-targets.log`, and `http-error-input-final-review.json`.
