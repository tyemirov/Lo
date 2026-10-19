# Confident Programming

This policy controls all agent work in this repository.

## Lo Repository Boundary

Lo is a self-contained project.
All Lo work must stay inside this repository, under all circumstances and for every task.
This boundary controls every section of this policy and all repository guides, plans, examples, and delegated instructions.
Shared templates and skills cannot authorize an exception.

- Keep research, planning, implementation, documentation, tests, validation, and artifact preparation inside Lo.
- Never use another repository as a reference, dependency checkout, implementation target, or acceptance environment.
- Never change files, installed scripts, configuration, caches, or application state outside Lo.
- Never install Lo output outside this repository as part of a Lo task.
- Keep test fixtures, temporary files, logs, caches, generated files, and build output inside Lo.
- Configure each tool's temporary directories, caches, and output paths before execution.
- Examine resolved task paths before access. Reject symlinks or path traversal that cross the repository boundary.
- Use fixtures inside Lo for examples that represent external paths, commands, or services.
- Never run cleanup examples against real user data or application caches.
- Use the existing primary checkout. Do not create or use another checkout or worktree.
- Give every delegated agent this boundary. Keep its tools and child processes within the same boundary.
- Examine child processes and validation commands for external effects before execution.
- If an operation cannot obey this boundary, stop that operation and report the exact limitation.
- Do not expand task scope to another repository or external environment to remove a blocker.
- Preserve this boundary when shared governance templates propose different instructions.

An example's application name does not add another project to the task.
Release, publication, deployment, integration, or acceptance requirements do not authorize external operations for Lo work.

## Temporary Task Files

- Put temporary diagnostic files in `_scratch/<task-id>/`.
- Use a unique task identifier for each directory.
- Keep `/_scratch/` in the repository `.gitignore`.
- Record the directory and its task in the task plan before use.
- Keep files from an interrupted task until that task resumes or its owner authorizes removal.
- Before task completion, preserve useful results in maintained tests, tools, or documentation.
- Remove only the temporary directory owned by the completed task.
- Verify that this directory is absent before reporting task completion.
- Preserve directories owned by other tasks.
- If removal fails, report the remaining path and keep task completion pending.

## Operator Rules

- Validate only at edges: I/O, HTTP, CLI, DB adapters, browser bootstrap, imported files, and other external boundaries.
- Design HTTP APIs as resource-oriented REST APIs. Use standard HTTP methods, status codes, and semantics.
- For gRPC APIs, obey protobuf service and RPC conventions. REST constraints do not apply.
- Make illegal states unrepresentable with domain types, smart constructors, dataclasses, enums, or closed action objects.
- Fail fast on impossible states.
- Wrap boundary errors with operation and subject context.
- After boundary validation, do not repeat validation in core modules.
- Keep interfaces narrow. Prefer domain types instead of loose strings, maps, booleans, or `any` values.
- Centralize reusable literals: paths, operation names, event names, config keys, status values, and shared messages.
- Tests target public contracts and invariants, not defensive branches.
- Prefer black-box integration and end-to-end tests through real entry points.

## Automatic Data Updates And Persistence

This is a binding product principle for every user interface.

- Load current data automatically when a view opens and when its relevant data changes.
- Update stale data automatically after reconnection and when the user returns to the view.
- Never require or expose refresh or reload controls for data synchronization.
- Persist valid user edits automatically. Never require or expose save controls for data persistence.
- Show pending, saved, validation, conflict, and failure states without requiring a manual refresh or save.
- Preserve unsaved edits during background updates, request failures, and concurrent changes.
- Serialize writes and retain newer edits when an earlier write completes.
- Keep explicit controls for user intent, such as creation, deletion, activation, and confirmation of consequential actions.
- Do not use an explicit action as a substitute for automatic data persistence.
- Verify automatic updates, persistence, error recovery, and account isolation through public interface tests.

## Test-Driven Development

- Use test-driven development with an inverted test pyramid.
- Integration tests are the primary test layer.
- Use focused unit tests for complex algorithms, calculations, and isolated logic when useful.
- Require integration coverage of public behavior for product acceptance.
- An integration test must use a real public entry point.
- For a behavior change, start with the integration test that represents the required public behavior.
- Run the new or changed integration test before you change production code.
- Confirm that the integration test fails because the required behavior is absent or incorrect.
- After this failure, use focused unit tests to guide complex internal implementation when useful.
- Change the minimum production code necessary to make the integration test pass.
- Refactor only while the applicable integration tests pass.
- For a refactor with no behavior change, run the applicable integration tests before you change production code.
- If focused coverage is absent, add a characterization test before the refactor.
- Use deterministic local infrastructure for repository-owned databases, filesystems, queues, servers, and browsers.
- At an external provider boundary, use a local protocol implementation with state stored inside Lo.
- Do not use a provider sandbox or live-provider operation for Lo acceptance.

## Dependency Injection In Integration Tests

- Use dependency injection for integration scenarios that are difficult to reproduce.
- Inject the dependency that creates the test condition.
- Use controlled clocks, dependency failures, or unusual responses when the scenario requires them.
- Keep the product logic under test and its related internal interactions real.
- Assert observable results through the public contract.
- Keep integration coverage with real dependency implementations.
- Injected scenarios prove behavior under the specified conditions. They do not prove actual provider connectivity.
- Report actual provider connectivity as unverified. Do not cross the repository boundary to qualify it.

## Prohibited Patterns

- Silent fallbacks, best-effort behavior, legacy aliases, and compatibility reads unless an explicit product requirement says the behavior is current.
- Duplicated validation inside core modules.
- Exporting invalid zero-values as usable domain objects.
- Swallowing errors.
- Increasing waits or timeouts as the primary fix for flakiness.
- Boolean parameters that switch unrelated behaviors.
- Hardcoded workflow, path, event, or message literals when a canonical constant or backend payload exists.

## Device Validation

- Never require a physical device or physical-device access for any task or gate, under any circumstances.
- Apply this prohibition to development, tests, validation, acceptance, issue closure, release, publication, and deployment.
- Never ask the user to provide, obtain, connect, or arrange access to physical devices or physical-device test services.
- Use simulators, emulators, and automated browsers for device validation.
- Accept these environments as sufficient for device validation and acceptance.
- Remove physical-device requirements and blockers from the selected task and its current acceptance records.
- Never replace a removed physical-device gate with another hardware prerequisite.
- Report the actual test environment and observed results without claiming physical-device execution.

## File Permission Boundary

- File permission modes are outside agent scope.
- Never examine, validate, compare, require, change, or record a file permission mode.
- Never use a file permission mode in acceptance, security, credential, execution, publication, deployment, or failure analysis.
- The values `0600` and `7777` have no governance meaning.
- This rule does not change service authorization or operation authority.

## Selected Manifest Contract

Apply this section when the task changes or validates a selected application manifest.

- Keep the selected application manifest versionless.
- Keep `owner`, `release`, and `resources` as the current baseline fields.
- Apply the current iOS baseline below before the manifest preservation rules.
- For other manifest contracts, accept each manifest that an earlier versionless gateway accepted.
- For those contracts, keep each accepted field name, type, requirement, and function.
- A field added to an existing shape must be optional.
- A field added to an existing shape must have one canonical default.
- Normalize that default before manifest identity calculation.
- Add a resource kind only with one closed shape.
- Reject unknown fields and `schema_version`.

### Current iOS Baseline

The operator selected a fresh iOS contract on September 10, 2026.
Previous iOS adapter acceptance does not constrain this contract.

- Require `build_system: local` and a repository-relative `.mjs` path in `build.ios`.
- Build and sign iOS artifacts locally through the shared native builder.
- Keep prepared artifacts inside Lo. The repository boundary prohibits external publication operations.
- Reject iOS `.sh` adapters, Xcode Cloud requests, and cloud publication receipts.
- Do not add compatibility dispatch, obsolete aliases, or migration requirements for the removed iOS contract.
- Preserve the declared TestFlight or App Store destination and its export restriction.

This baseline does not authorize removal of stored releases, credentials, or deployment state.

## Repository Lifecycle Work

- Apply the Lo repository boundary to lifecycle tooling, artifact preparation, and related validation.
- Keep lifecycle source changes, fixtures, artifacts, and evidence inside Lo.
- Do not operate installed applications, update external operator state, or use another repository for lifecycle work.
- Do not execute publication, deployment, or installation that changes state outside Lo.
- If a lifecycle requirement needs external operations, report that limitation without executing those operations.

## Credential Discovery

Apply this gate when the selected task requires credentials.

- Identify each required environment variable from the active command and repository contract.
- Inspect the process environment before you request a login.
- Inspect repository private environment files before you report a credential blocker.
- Include ignored `.env`, `.env.*`, and `*.env` files in the authorized repository roots.
- Treat tracked example and sample environment files as documentation only.
- Search only for exact variable names. Do not print, copy, or record secret values.
- When a command declares one repository environment file as its input, clear
  its owned process variables and source only that file.
- Use the command's standard environment lookup. Do not add a credential parser
  or another input channel.
- When available, use a non-mutating authentication command to verify the discovered value.
- Request new credentials only after each authorized existing input fails verification.
- Report a credential blocker only after you complete this gate.

## Issue Closure

- Close an issue when its required implementation, documentation, and code validation are completed.
- Keep release, publication, deployment, production DNS/TLS, and live acceptance outside implementation issue acceptance criteria.
- Do not keep an implementation issue open or blocked because production operations are not completed.
- Record pending production operations in a runbook or separate operational record.
- Validate deployment tooling and infrastructure configuration through the required isolated integration tests.

Failed or missing required code validation prevents closure.
Production operations outside the task do not remove code validation requirements.

## Validation

- Use repository-native `make` targets.
- Do not run a pre-edit or per-issue `make ci` baseline.
- Preserve the expected failing integration-test result as implementation evidence.
- During the change, run the smallest public-entrypoint target that validates the changed contract.
- After the last stack change, run `make ci` once at the documented stack completion checkpoint.
- If this run reports an error, run the target that reports the error during the correction.
- After the last correction, run `make ci` once.
- Run `make verify` only when the operator explicitly selects the complete qualification lane.
- When `make ci` includes `make fmt`, `make lint`, and `make test`, use its result for those targets.
- During the change or error diagnosis, run the necessary component target.
- Run a component target when `make ci` does not include the necessary check.
- For documentation-only work, run the applicable document and repository checks.
- For `.mprlab/`-only work, run the Governor check and `git diff --check`.
- These are the repository checks for `.mprlab/`-only work. Changed prose also requires the documentation-language review.
- For read-only work, use source facts and run only the necessary checks.
- For frontend behavior, verify through a browser test when the behavior is user-visible.
- For services and CLIs, verify through HTTP, CLI, or public API entry points.

## Documentation Language

- Write new or changed English technical prose in ASD-STE100 Simplified Technical English, Issue 9.
- Read `.mprlab/AGENTS.DOCS.md` and `.mprlab/TERMINOLOGY.md` before you write technical prose.
- Apply this rule to PRDs, architecture documents, issues, plans, policies, ADRs, READMEs, runbooks, and API documents.
- Do not change technical meaning to make the language simpler.
- Run `prepare-ste-reference --cache-dir <task-directory>/ste-reference --json` to retrieve and verify the official Issue 9 PDF.
- Keep the reference cache inside Lo's ignored task directory.
- Run the skill `check-ste` script on each technical document that you change.
- The producing agent must review Part 1 writing rules and the Part 2 dictionary.
- Do not assign the reference retrieval or language review to the end user.
- If the official reference is not available, report a blocker and do not claim compliance.

## Language Rules

### Go

- Use smart constructors returning `(Type, error)` when a type has invariants.
- Do not export invalid zero-values.
- Wrap errors with `%w`.
- Prefer integration tests through real HTTP, CLI, or package entry points.
- `make lint` must include `go vet`, `staticcheck`, and `ineffassign` when those tools are part of the repo contract.

### Python

- Use `@dataclass(frozen=True)` or Pydantic when already in use.
- Validate in constructors or edge adapters.
- Use type hints throughout.
- Use pytest for integration, end-to-end, and focused unit tests under the shared test-driven development rules.

### JavaScript And Frontend

- Put `// @ts-check` at the top of new or edited JavaScript modules when the repo uses checked JS.
- Use JSDoc typedefs for domain objects and payload contracts.
- Components render validated state and emit intent.
- Backend clients own request construction and response validation.
- User-visible behavior belongs in browser or integration coverage.

## Self-Check

Before claiming completion:

- External inputs are validated once at the edge.
- Core modules consume validated domain values.
- Error paths include operation and subject context.
- Reusable literals are centralized.
- Public behavior is covered through public entry points.
- Repo-native validation was run or a concrete blocker is documented.
