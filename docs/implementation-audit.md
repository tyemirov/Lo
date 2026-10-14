# Compiler Implementation Audit

This audit maps the compiler design to source code, public acceptance tests, and recorded validation.
Its scope is the six stages in [the compiler design](compiler-design.md).
The audit includes the runtime contracts in specification sections 23.1 through 23.7.
The six implementation stages passed acceptance.
The [final gate](evidence/audit-green.txt) includes the additional resource-limit and client-disconnection tests.
F001 extends this audit with the native compiler and shared runtime described below.

## Evidence Sources

| Source | Coverage |
| --- | --- |
| [CLI tests](../tests/integration/cli_test.go) | Command inputs, diagnostics, source boundaries, syntax, and all 63 static fixtures. |
| [Verifier tests](../tests/integration/verifier_test.go) | Types, scopes, fallibility, handlers, effects, and all six specification examples. |
| [Catalog tests](../tests/integration/catalog_test.go) | A source fixture and expected checked type for every built-in operation. |
| [Graph tests](../tests/integration/graph_test.go) | Compiler publication, references, spans, conversions, immutability, and static-only behavior. |
| [Inspection tests](../tests/integration/inspect_test.go) | Report fields, references, conditions, capabilities, errors, and deterministic output. |
| [Runtime tests](../tests/integration/run_test.go) | CLI values, serialization, recoverable errors, traces, file reads, and host limits. |
| [Runtime catalog tests](../tests/integration/runtime_catalog_test.go) | Expected runtime behavior for all 100 built-in operations. |
| [Evaluator tests](../tests/integration/eval_test.go) | Evaluation order, branch selection, repeated reads, error precedence, cancellation, and immutable results. |
| [File tests](../tests/integration/activation_test.go) | Commit order, failure handling, authorization, and the executable File workflow. |
| [HTTP tests](../tests/integration/serve_test.go) | Real CLI processes, local HTTP requests, readiness, failures, and shutdown. |
| [Adapter tests](../tests/integration/adapter_test.go) | File cancellation, concurrent append, partial startup, nested servers, and forced shutdown. |

The test names below identify executable fixtures in these files.
`TestConformance` executes every entry in the [static manifest](../testdata/conformance/manifest.json).
The two catalog tests reject missing built-in coverage.
Runtime fixtures use stages 5 and 6, as identified in the stage tables below.
Their source programs and expected values are in the named tests.

## Compiler Contracts

| Design requirement | Source and acceptance evidence |
| --- | --- |
| Go toolchain, CLI, and native build targets | [go.mod](../go.mod), [Makefile](../Makefile), and `TestMain` build the real CLI. |
| One checked graph for inspection and execution | [compiler](../internal/compiler/compiler.go), `TestCheckedGraphContracts`, and `TestInspectionResources`. |
| Source identity and byte spans | [source](../internal/source/source.go), `TestFirstVerifierSlice`, and `TestParserExternalInputs`. |
| Scalar-based line columns and CRLF input | `TestParserExternalInputs` checks byte offsets, Unicode columns, and CRLF acceptance. |
| Source size and nesting limits | `TestParserExternalInputs` rejects oversized source and excessive nesting. |
| Handwritten lexer and recursive-descent parser | [syntax package](../internal/syntax), `TestSyntaxStructure`, and syntax cases in the manifest. |
| Statement, argument, and match source order | `TestCheckedGraphContracts`, `TestEvaluatorReadOrder`, and `TestEvaluatorMatchRegions`. |
| Original spans through lowering | `TestCheckedGraphContracts/pipe-and-source-spans` checks the call and its first argument. |
| Normative precedence and multiline pipes | `TestSyntaxStructure`, manifest `multiline-pipe`, and all specification examples. |
| Malformed input prevents graph publication | `TestConformance`, `TestParserExternalInputs`, and `TestCheckedGraphContracts/diagnostic-prevents-publication`. |
| Flow declarations before body resolution | [checker](../internal/check/check.go), `forward-handler`, and nested-flow cases in `TestVerifierContracts`. |
| Parameters before body statements | [resolver](../internal/resolve) and parameter cases in the static manifest. |
| Value references require earlier local bindings | `TestVerifierEdgeContracts/local-forward-value` and `TestEvaluatorReadOrder`. |
| Duplicate and predeclared names fail | Manifest `duplicate-binding`, `duplicate-parameter`, and `shadow-builtin`. |
| Unknown names, types, and processors fail | Manifest `unknown-call`, `unknown-reference`, and `TestVerifierRejections/unknown-type`. |
| Nested flows do not capture outer values | Manifest `capture`, plus `top-capture`, `future-capture`, and `handler-capture` tests. |
| Nested declarations have lexical visibility | `TestVerifierContracts/nested-flows` and `TestVerifierEdgeContracts/nested-flow-not-visible`. |
| Direct calls, callbacks, and handlers resolve to flow identities | [checker](../internal/check) and `TestVerifierBuiltinContracts`. |
| Direct and indirect cycles fail with related spans | `compileFlow` records the active flow stack. Manifest `recursion` and verifier cycle tests reject cycles. |
| Stored adapter references participate in cycle checks | `TestVerifierEdgeContracts/adapter-cycle`. |
| Stored adapter references do not invoke handlers during construction | `TestCheckedGraphContracts/effect-summaries-and-adapter-construction` and `TestInspectionUncommittedServer`. |
| Typed representations replace unrestricted type strings | [types package](../internal/types) supplies private atomic, container, record, and fallible representations. |
| Expected types guide empty and dependent constructors | `empty-context`, `empty-list-context`, `fallible-elements`, and `map-fallible-value` verifier cases. |
| Exact records enforce field names and types | Manifest `wrong-record`, plus extra-field, null-field, and duplicate-field verifier cases. |
| Error remains nominal | `TestVerifierEdgeContracts/nominal-error` and runtime `Error.new` acceptance. |
| Null assigns only to Null | Manifest `null-argument`, catalog `List<Null>`, and runtime null acceptance. |
| Map keys and range endpoints use comparable domains | Manifest `invalid-map-key`, plus range and map rejection tests. |
| String supplies List of Scalar inputs | Catalog `List.filter` and `List.length`, plus `TestRunListProcessors/filter-string`. |
| Calls validate names, counts, order, and callback parameters | Manifest argument cases, `TestVerifierRejections`, and both catalog tests. |
| Adapter handlers have the required signatures | Manifest `missing-handler`, plus wrong HTTP handler and error-handler tests. |
| Total flows reject unresolved failure | Manifest `fallible-total`, `map-get-total`, and verifier commit restrictions. |
| Propagation has explicit IR nodes | `TestCheckedGraphContracts/propagation-and-stored-fallibility`. |
| Success, failure, and stored fallibility remain distinct | `TestRunStoredFallibility` and nested verifier fixtures. |
| Match branches unify under the canonical rules | Manifest `wrong-branch`, plus match fallibility and stored-failure verifier cases. |
| One catalog owns signatures, operation names, effects, and error kinds | [catalog](../internal/catalog/catalog.go), both catalog tests, and `TestInspectionHTTPBoundaryErrors`. |
| Transitive effects include executed calls and callbacks | Manifest `transitive-effect`, verifier effect cases, and graph effect-summary tests. |
| Unused declarations receive validation without adding enclosing effects | `invalid-unused`, `nested-unused-effect`, and `TestInspectionUncommittedServer`. |
| Checked references identify symbols, operations, regions, and flow inputs | [IR](../internal/ir/ir.go), [graph index](../internal/inspect/index.go), and graph contract tests. |
| Pipe lowering supplies the first argument | `TestCheckedGraphContracts/pipe-and-source-spans` and runtime catalog operations. |
| Named argument evaluation stays in source order | `TestCheckedGraphContracts/named-argument-evaluation-order` and `TestEvaluatorReadOrder`. |
| Match evaluates only reached tests and the selected body | `TestEvaluatorMatchRegions`, `TestRunValues/match-lazy`, and case-head inspection tests. |
| Each commit keeps its identity and order | `TestCheckedGraphContracts/commit-sites`, `TestInspectionResources`, and `TestFileCommitOrder`. |
| Failed evaluation stops dependent work | `TestEvaluatorErrorPrecedence`, `TestRunFallibleListVariants`, and `TestFileCommitFailure`. |
| Reads remain distinct and do not execute speculatively | `TestEvaluatorReadOrder`, `TestEvaluatorMatchRegions`, and `TestRunFileReads`. |
| Source diagnostics prevent graph publication | `TestCheckedGraphContracts/diagnostic-prevents-publication`. |
| Graph construction stays inside the compiler boundary | The checker is the only production caller of `ir.NewProgram`. The CLI accepts source, then calls `compiler.Check`. |
| Published graph views are immutable | `TestCheckedGraphContracts/immutable-public-views`. |
| Compiler commands do not access program resources | `TestVerifierDoesNotExecuteBoundaries` and `TestInspectionResources`. |

## Inspection and Runtime Contracts

| Design requirement | Source and acceptance evidence |
| --- | --- |
| Report nodes, bindings, flows, contexts, and entry points | [report schema](../internal/inspect/report.go), graph indexing, and `validateInspectionReferences`. |
| Report reads, targets, activations, errors, capabilities, and unused flows | `TestInspectionResources`, `TestInspectionBranchesAndCallbacks`, and `TestInspectionUncommittedServer`. |
| Known and dynamic target descriptions preserve source dependencies | `TestInspectionReturnedTargetsAndErrors` and `TestInspectionCalculatedTextLimit`. |
| Indirect and runtime-selected effects appear with conditions | `TestInspectionBranchesAndCallbacks`, `TestInspectionLoopCarriedTargets`, and `TestInspectionRecordTargetSelection`. |
| Repeated calls and commits keep different identities | `TestInspectionResources` and the context and reference assertions. |
| Handler analysis keeps effects and propagation boundaries | `TestInspectionReturnedTargetsAndErrors`, nested adapter tests, and HTTP boundary error tests. |
| Constructed Error data differs from propagated failure | `TestInspectionReturnedTargetsAndErrors` checks `value` and handled error sources. |
| Dynamic Error kinds remain explicit | `TestInspectionReturnedTargetsAndErrors` checks the kind expression description. |
| Uncommitted targets grant no activation capability | `TestInspectionUncommittedServer` and `TestInspectionRecordTargetSelection`. |
| Unused flow behavior grants no program capability | `TestInspectionResources` and `TestInspectionUncommittedServer`. |
| Text and JSON use one deterministic report | `report.Text`, `report.MarshalJSON`, and repeated CLI calls in `TestInspectionResources`. |
| Empty report collections serialize as arrays | `newReport` constructs each collection, and the reference validator accepts the serialized report. |
| Inspection limits reject without partial output | `TestInspectionContextLimit` and `TestInspectionCalculatedTextLimit`. |
| Immutable runtime values protect shared integers and collections | `TestEvaluatorImmutableResults` and [value representations](../internal/value/value.go). |
| All 52 built-ins have runtime behavior | `TestRunBuiltinCatalog` checks each catalog entry and its expected result or error. |
| Integer arithmetic preserves arbitrary precision | `TestRunValues/large`, `TestRunValues/immutable`, and signed arithmetic cases in `TestRunRangeOrder`. |
| Empty aggregates and duplicate map keys obey the specification | `TestRunValues`, `TestRunRecoverableErrors`, and runtime catalog cases. |
| Scalar, string, and range comparisons obey the specification | `TestRunRangeOrder` covers endpoint inclusion, reversed ranges, Unicode gaps, and string successors. |
| List context arguments and callback order are explicit | `TestRunListProcessors` covers map, filter, fold, flat_map, and unfold. |
| Lo error kinds survive propagation and list processing | `TestRunRecoverableErrors` and `TestRunFallibleListVariants`. |
| Flow and list trace frames precede handler entry | `TestRunRecoverableErrors`, `TestRunFallibleListVariants`, and [error values](../internal/value/error.go). |
| Outer failures differ from stored failures | `TestRunStoredFallibility` and `TestEvaluatorErrorPrecedence`. |
| Host cancellation, authorization, and limits bypass Lo handlers | `TestEvaluatorBoundaryHostFailures`, `TestEvaluatorCancellation`, and `TestRunHostLimitsAndAuthorization`. |
| Client disconnection does not stop the service | `TestServerDisconnectedErrorResponse` checks cancellation during a large error response. |
| Runtime JSON preserves numeric precision and every value category | `TestRunBuiltinCatalog`, `TestRunValues`, and `TestRunStoredFallibility`. |
| Text results and traces use the documented contract | `TestRunValues`, `TestRunCountLetterExample`, and `TestRunRecoverableErrors`. |
| A program without a return omits its result | `TestRunValues` and `writeExecution` in [shared CLI execution](../internal/runner/execute.go). |
| Source and invocation failures use stable exit classes | `TestCLIErrorBoundary`, `TestFirstVerifierSlice`, and `TestRunRecoverableErrors`. |
| Runtime options reject invalid or duplicate inputs | `TestRunHostLimitsAndAuthorization` and `parseArgs` in [the shared runner](../internal/runner/command.go). |
| Reads need a matching directory root | `TestRunFileReads` and `TestRunHostLimitsAndAuthorization`. |
| Path normalization and most-specific root selection occur at the adapter edge | `files.resolve` and `openRoots` in [the File adapter](../internal/adapter/files.go). |
| Root traversal and symlink escape fail | `TestRunHostLimitsAndAuthorization` and `TestFileCommitAuthorization`. |
| Invalid UTF-8 produces the specified Lo error | `TestRunFileReads` and `TestServeRequestValidation`. |
| Steps, integer magnitude, collection count, and flow depth have host limits | `TestRunHostLimitsAndAuthorization` and `TestRunResourceLimits`. |
| File bytes, expanded values, and cumulative value weight have host limits | `TestRunResourceLimits` and `TestRunExpandedValueLimit`. |
| String construction checks byte limits before publication | [Shared operations](../internal/runtimeops/builtin.go) and value weight checks in [execution sessions](../internal/execution/session.go). |

## Stage Acceptance

Each row covers the deliverables and acceptance criteria for its numbered stage.
The earlier tables supply the detailed compiler and runtime invariants.

| Stage | Deliverable or acceptance criterion | Evidence |
| --- | --- | --- |
| 1 | Resolve lexical and verifier rules in the specification. | Specification sections 17.1 and 22, with static fixtures. |
| 1 | Copy all six specification examples into source files. | Manifest `example-1` through `example-6` and `TestVerifierExamples`. |
| 1 | Index positive and negative conformance outcomes. | Static manifest fields `id`, `rule`, `command`, `exit`, `diagnostic`, and `stage`. |
| 1 | Cover transitive effects, multiline pipes, nested fallibility, and cycles. | Named manifest entries and verifier edge fixtures. |
| 1 | Identify runtime fixture groups and their stages. | Stages 5 and 6 in this table identify their executable fixture groups. |
| 1 | Separate resolved contracts from unspecified decisions. | Specification sections 22 and 23, plus the design authority and contract sections. |
| 2 | Pin Go and supply the module, CLI, and Make targets. | `go.mod`, `cmd/lo`, and `Makefile`. |
| 2 | Implement source spans, tokens, parsing, and parse diagnostics. | Source and syntax packages, `TestConformance`, and `TestParserExternalInputs`. |
| 2 | Accept list sum through `lo check`. | `TestFirstVerifierSlice` checks result type and exit status. |
| 2 | Reject an incompatible list with its span and expected type. | `TestFirstVerifierSlice` checks `List<Int>`, `List<String>`, line, and column. |
| 2 | Reject unknown processors and malformed source. | Manifest `unknown-call` and syntax rejection fixtures. |
| 2 | Parse every specification example. | Manifest `example-1` through `example-6`. |
| 2 | Reject unsupported partial-stage behavior explicitly. | The full verifier replaces the partial implementation. Unknown syntax, types, and operations still fail through the CLI. |
| 3 | Implement every type and built-in signature. | Verifier and catalog tests. |
| 3 | Implement scopes, handlers, fallibility, effects, and cycle detection. | Verifier fixtures and the compiler contract table above. |
| 3 | Construct checked graphs without adapter operations. | Graph contract and static boundary tests. |
| 3 | Accept all six examples and all static fixtures. | `TestVerifierExamples` and `TestConformance`. |
| 3 | Reject hidden effects, captures, wrong handlers, records, and null arguments. | Verifier contract and rejection fixtures. |
| 3 | Verify pipe order, propagation, and unused bodies. | Graph tests and verifier edge fixtures. |
| 3 | Prevent rejected source from publishing a graph. | `TestCheckedGraphContracts/diagnostic-prevents-publication`. |
| 4 | Supply deterministic text and JSON inspection. | Report schema and `TestInspectionResources`. |
| 4 | Include indirect effects, dynamic targets, and possible match branches. | Inspection callback, branch, returned-target, and loop-carried-target tests. |
| 4 | Distinguish construction, activation, and repeated commits. | Inspection resource and uncommitted-server tests. |
| 4 | Include known and dynamic error kinds. | `TestInspectionReturnedTargetsAndErrors` and `TestInspectionHTTPBoundaryErrors`. |
| 4 | Supply the File workflow and documented report. | `examples/agent_file_workflow.lo`, `TestInspectionExample`, and the design's File Workflow Inspection section. |
| 5 | Implement immutable values, graph evaluation, flows, matches, and list operations. | Runtime catalog, evaluator, and list processor tests. |
| 5 | Implement errors, self-handlers, and trace frames. | Runtime error and fallible-list fixtures. |
| 5 | Implement authorized reads and UTF-8 decoding. | File read, host authorization, and cancellation fixtures. |
| 5 | Supply `lo run` and its result formats. | CLI runtime tests and the execution command contract. |
| 5 | Execute list sum and count-letter examples. | `TestRunValues` and `TestRunCountLetterExample`. |
| 5 | Verify large integers, branch selection, read order, and repeated reads. | Large-value, match-region, and evaluator read-order tests. |
| 6 | Resolve write and server capabilities. | CLI option, File authorization, and HTTP startup tests. |
| 6 | Activate each evaluated File commit once in order. | `TestFileCommitOrder` checks exact bytes after repeated commits. |
| 6 | Prevent uncommitted writes and stop after unhandled failures. | `TestFileCommitAuthorization` and `TestFileCommitFailure`. |
| 6 | Deliver activation errors to self-handlers with traces. | `TestFileCommitFailure` and `TestServeSelfHandledFailures`. |
| 6 | Implement HTTP values, dispatch, and response serialization. | `TestServeHTTP`, `TestServeEchoExample`, and bodyless response tests. |
| 6 | Handle Lo errors and ordinary request boundary errors. | HTTP Lo-error, request-validation, body-limit, and truncated-request tests. |
| 6 | Validate responses and reject an invalid error response. | `TestServeResponseBoundary` and `TestServeFatalBoundaryFailures`. |
| 6 | Reject denied or failed startup and clean partial activation. | `TestServeStartupFailures` and `TestServerPartialStartupCleanup`. |
| 6 | Support repeated activation and concurrent requests. | `TestServeRepeatedActivationAndConcurrentRequests` and `TestServerActivationInRequest`. |
| 6 | Implement cancellation and orderly shutdown. | File cancellation, active-request shutdown, pending-connection shutdown, and forced shutdown tests. |
| 6 | Bound active server count. | `TestServeServerLimit`. |
| 6 | Execute File and HTTP examples. | `TestFileWorkflowExample`, `TestServeEchoExample`, and [the unchanged division example run](evidence/http-example.txt). |
| 6 | Qualify the declared host platforms. | [macOS race evidence](evidence/activation-http-race.txt) and [Linux test evidence](evidence/linux-tests.txt). |

## Package and Artifact Review

Every planned package exists at its specified path.
The compiler and inspector have no runtime adapter dependency.
The CLI composes the checked graph, evaluator, and adapters.
IR construction occurs in the checker, before publication through `compiler.Check`.
Private representations and copied collection views preserve published graph and value contents.

| Artifact | Review result |
| --- | --- |
| `cmd/lo/` | Source compiler and native build commands. |
| `internal/runner/` | Runtime arguments, streams, diagnostics, signals, and lifecycle. |
| `internal/source/`, `internal/syntax/` | Source identity, spans, tokens, AST, and parser. |
| `internal/resolve/`, `internal/types/`, `internal/check/` | Symbols, domain types, type rules, flow analysis, and graph construction. |
| `internal/catalog/`, `internal/ir/` | Canonical operations and immutable checked graph representations. |
| `internal/inspect/` | Static traversal, abstract values, report schema, and text output. |
| `internal/value/`, `internal/eval/` | Immutable runtime values and interpreter graph execution. |
| `internal/execution/`, `internal/runtimeops/` | Shared budgets, propagation, program entries, and runtime operations. |
| `internal/adapter/` | Authorized files, HTTP dispatch, listeners, and cleanup. |
| `internal/diagnostic/`, `internal/compiler/` | Structured diagnostics and compiler entry points. |
| `tests/integration/`, `testdata/conformance/` | Public acceptance tests and indexed static fixtures. |
| `examples/` | Specification examples and executable File and HTTP workflows. |
| `Makefile` | Compiler, native, race, and benchmark targets. |

## Validation Gates

The repository uses integration tests as its primary acceptance layer.
File tests use temporary local resources.
HTTP tests use local listeners and real requests.
Evaluator tests inject only the external boundary necessary to observe order or a specified failure.
The compiler, graph, interpreter, and handler logic stay real in those tests.

The stage evidence records the initial failures and completed checks.
The final audit gate passed with all fixtures and the new resource-limit cases.
No fixture group counts as passed before its test completes.
No native code backend, optimization pass, or persistent executable format belongs to these six stages.

| Gate | Result and evidence |
| --- | --- |
| macOS arm64 `make ci` | Passed formatting, `go vet`, all tests, and the CLI build. [Final output](evidence/audit-green.txt). |
| macOS arm64 race checks | Passed File, HTTP, and HTTP inspection tests with the CLI and test process compiled with `-race`. [Output](evidence/activation-http-race.txt). |
| Linux arm64 full suite | Passed `make test TEST_ARGS='-count=1 -timeout=120s'` in the pinned Go container. [Output](evidence/linux-tests.txt). |
| Original HTTP division example | Returned HTTP `400` and completed shutdown with exit status `0`. [Output](evidence/http-example.txt). |
| Changed technical prose | Mechanical checks passed for new and changed text. Unchanged README and specification findings remain. |
| Repository documentation | Local links and whitespace checks passed. Existing Governor drift remains outside this implementation change. |

The Linux container image is:

```text
golang@sha256:8d48e12ec56735e9358640898b9d9b9fcca110612ed8a5567438c0a1baa24e66
```

The race command is:

```bash
GOFLAGS=-race make test-integration \
    TEST_ARGS='-run "TestServe|TestServer|TestFile|TestInspectionHTTP" -count=1 -timeout=90s'
```

The [disconnect regression](evidence/http-disconnect-red.txt) records the failure before its correction.
The corrected adapter uses connection cancellation when response delivery fails.
The audit found no remaining implementation requirement within the six-stage scope.

## Native Compilation

F001 adds direct Go generation and standalone native artifacts.
The [native results](native-compilation-results.md) record semantic tests, timing gates, resource measurements, and target environments.
The [native contract](native-compilation-contract.md) owns the build limits and logical charge order.

| Requirement | Implementation and evidence |
| --- | --- |
| Shared execution contracts | `internal/execution`, `internal/identity`, `internal/runtimeops`, and `internal/runner`. |
| Direct generated execution | `internal/codegen` emits flow functions, local variables, branches, list loops, and derivative operations. |
| Compiler-free runtime | `internal/derivative` supplies VJP arithmetic without compiler plans. Artifact symbol checks reject compiler and interpreter packages. |
| Isolated artifact construction | `internal/nativebuild` uses a controlled toolchain environment, temporary module, process-group cancellation, and atomic output replacement. |
| Runtime source identity | `tools/runtimebundle` emits the embedded bundle and content manifest. CI verifies the bundle before regeneration. |
| Public native behavior | `make test-native` compiles CLI execution fixtures and uses their existing expected results. |
| Limits and effects | Native differential tests compare diagnostics, results, and file effects at budget boundaries. |
| Target execution | The complete native suite passed on macOS arm64 and Linux arm64. |
| Concurrent runtime behavior | Generated artifacts passed the selected HTTP and execution race tests on both targets. |
| Repository validation | [Final CI](evidence/native-ci.txt) passed formatting, static analysis, interpreter tests, native tests, and the compiler build. |

F001 retains the current language, value, authorization, and error contracts.
The native backend adds no language syntax or automatic backend selection.
