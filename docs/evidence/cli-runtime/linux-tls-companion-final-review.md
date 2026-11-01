# Linux Companion Fixture Review

Status: passed for the four maintained files and focused checks.
No blocking finding remains.

All maintained byte digests match the approved final draft.
The runner validates the executable and companion as local ELF files before it packages the guest.
It gives the companion one fixed path, `/companion`.
The driver receives that path as its first argument.
It starts the child without an embedded image or an inherited image descriptor.

Companion requests always use the controlled software guest.
Only ordinary requests without a companion keep the existing direct Linux path.
The driver uses the guest initialization process for `/proc`.
Its loopback setup and TLS files stay inside the guest.
An absent or unsupported preparation cannot start the driver on the host.

The public regression failed before the runner accepted `--companion`.
After the correction, the child returned status 7 and both exact output streams.
The trusted, wrong-name, and untrusted TLS scenarios remain unchanged.
The driver still requires exactly one successful server request.

The companion, TLS, and four Linux core cases passed in 17.828 seconds.
The native runtime tools passed all twenty tests in 5.67 seconds.
The runtime source and bundle remain unchanged.
The full stack CI remains pending.

Evidence and exact byte digests appear in `linux-tls-companion-final-review.json`.
