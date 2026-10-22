# Native File, Process, And HTTP Adapters

I009 implements adapters in the static native runtime.
The [runtime guide](native-runtime.md) gives the LLVM build procedure.
The [runtime contract](native-runtime-contract.md) owns ABI and lifetime requirements.
The adapter implementation keeps the current language operations and execution capabilities.
F017 and I011 own the installed standalone compiler acceptance criterion.
The [adapter receipt](evidence/native-adapters/validation.json) records build identities, test results, and qualification limits.

## Execution Capabilities

An artifact starts without file, process, or HTTP capabilities.
The artifact accepts the current capability options at execution.
An immutable target does not supply authority.
Only an explicit `commit` activates a target.

| Adapter | Execution options | Contract |
| --- | --- | --- |
| File reads | `--read-root` | Confine reads to an opened directory root. |
| File writes and appends | `--write-root` | Confine changes to an opened directory root. |
| Directory observations | `--list-root` | Keep directory identity and request identity. |
| File removal | `--remove-root` | Validate the ordered selection before removal. |
| Open-file probe | `--open-files-root` | Use the fixed probe with its executable identity and bounded output. |
| Process execution | `--process-allow`, `--process-launcher`, `--process-read-root` | Authorize explicit executable identities and directory roots. |
| HTTP client | `--http-allow` | Authorize the selected method and HTTP origin. |
| HTTP server | `serve --listen` | Authorize the port. |

File operations keep opened directory roots.
File observations belong to one request.
Removal rejects foreign observations, changed identities, duplicate names, and invalid entry kinds.
Process registrations and the open-file probe protect their executable paths from file changes.
Cancellation stops blocked file work and prevents subsequent commits.

## Process Execution

Darwin arm64 supplies the current qualified process host.
Linux arm64 keeps `runtime.process-unavailable`.
Pure process constructors are available on both targets.

The Darwin artifact contains the process launcher.
The operator can select that artifact with `--process-launcher` and its absolute path.
An explicitly selected external launcher uses the current identity checks.
The embedded launcher lets the artifact execute without a Lo installation.
Neither route adds an implicit capability.

The launcher keeps the private configuration channel, confinement, execution witness, and permit sequence.
Process results keep termination, standard output, and standard error.
Failure diagnostics keep safe progress without command arguments, environment values, or captured output.
A completed activation remains visible after a later execution limit.

Each process operation keeps its resources until cleanup completes.
A timeout does not release an unresolved owner.
The CLI reports a bounded close failure before its final ownership wait.
Unresolved ownership prevents another process activation in the same session.

## HTTP Execution

The client uses one connection attempt to the selected authorized address.
It does not follow redirects or use environment proxy settings.
HTTP/1.1 framing, response limits, cancellation, and progress are explicit runtime contracts.
TLS uses pinned Rustls and ring sources.
Darwin uses system certificate verification through Security and CoreFoundation.
This path keeps the current platform policy for intermediate certificate retrieval and validates the requested hostname.
Linux uses its declared system certificate inputs.

A server starts its listener during `commit`.
Startup code can send a request to that listener before the CLI readiness event.
Each request receives a different context, execution budget, and file-observation identity.
Body reads and callback execution use the same request deadline.
An error callback runs at most once for one request failure.
Terminal host failures bypass Lo error handlers.

The service implements concurrent requests, persistent connections, and pipelined requests.
Shutdown closes idle connections and sets a time limit for active requests.
Cancellation then stops remaining request work.
The session keeps worker ownership until cleanup completes.
DNS work uses a bounded service that keeps worker handles until shutdown.

## Verification

Run the adapter checks through real LLVM artifacts:

```sh
make test-native NATIVE_RUNTIME_STATE=_scratch/native-runtime \
  TEST_ARGS="-run 'TestLLVMFile|TestLLVMLinuxFile|TestLLVMProcess|TestLLVMLinuxProcess|TestLLVMHTTP|TestLLVMLinuxHTTP|TestServe|TestHTTPClient' -timeout 20m -count=1" </dev/null
```

The Linux checks use QEMU software execution.
Loopback servers and test certificate inputs stay inside the test environment.
Native ABI fixtures inject host failures with real runtime ownership and commit behavior.
Tests that inject Go evaluator dependencies do not prove native artifact behavior.
TLS fixtures use local certificate chains without external intermediate retrieval.
These fixtures do not qualify external provider connectivity.

| Verification group | Required result |
| --- | --- |
| File authority | Reject denied access, traversal, and directory replacement. |
| File observations | Reject foreign sessions and stale selections before an invalid removal. |
| File cancellation | Stop a blocked operation and prevent the next commit. |
| Process execution | Keep declared inputs, termination, receipts, and safe diagnostics. |
| Process ownership | Keep ownership of late operations and unresolved cleanup. |
| HTTP authority | Reject denied methods, origins, addresses, and untrusted certificates. Do not follow redirects. |
| HTTP service | Keep startup, request isolation, error handling, cancellation, and shutdown behavior. |
| Resource lifetime | Release request contexts and keep only live operation resources. |
| Numerical regression | Verify backpropagation, 200-step training, 500-step XOR, and checkpoint resumption. |
