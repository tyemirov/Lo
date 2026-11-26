# Repository Policy Sections

## Lo Repository Boundary

Lo is a self-contained project.
Keep Lo project files and task state inside this repository.
This boundary controls every section of this policy and all repository guides, plans, examples, and delegated instructions.
Tools and skills can be in any location.

- Use tools and skills from any location, including system directories, user directories, other repositories, and symlink targets.
- Read tool and skill source files, instructions, dependencies, and references when necessary to use them.
- Never prevent tool use or ask for user approval because of its location.
- Keep project research, plans, implementation, documentation, tests, validation results, and artifacts inside Lo.
- Never use another project's application source as a Lo reference, dependency checkout, implementation target, or acceptance environment.
- Tool and skill access remains permitted when their files are in another project's repository.
- Never change files, installed scripts, configuration, caches, or application state outside Lo.
- Never install Lo output outside this repository as part of a Lo task.
- Keep test fixtures, temporary files, logs, caches, generated files, and build output inside Lo.
- Configure each tool's temporary directories, caches, and output paths before execution.
- Examine resolved paths for project files and task state before access.
- Reject paths that move project files or task state outside Lo.
- Permit external tool and skill paths, including symlink targets.
- Use fixtures inside Lo for examples that represent external paths, commands, or services.
- Never run cleanup examples against real user data or application caches.
- Use the existing primary Lo checkout. Do not create or use another Lo checkout or worktree.
- Give every delegated agent these rules, including tool access from any location.
- Keep delegated project files, task state, and child-process output inside Lo.
- Examine child processes and validation commands for external effects before execution.
- If an operation cannot obey this boundary, stop that operation and report the exact limitation.
- Do not expand Lo application work to another repository or external environment to remove a blocker.
- Preserve this boundary when shared governance templates propose different instructions.

An example's application name does not add another project to the task.
Release, publication, deployment, integration, or acceptance requirements do not authorize external operations for Lo work.
External tools and skills remain available for Lo work.

## Reviews And Specialized Agents

The user gives standing authorization for reviews and specialized agents in the current task.

- Start an independent review or specialized agent when it helps complete the current task.
- Do not ask for additional user approval for a review, agent startup, or task delegation.
- Include the task scope and applicable repository rules in delegated instructions.
- Apply the same tool access rules to the producing agent and delegated agents.
- Keep review artifacts and delegated task state inside Lo.
- Use existing task authorization for delegated actions.
- If an action exceeds the authorized scope, get user approval for that action.

Preserve these rules when shared templates or skills propose different instructions.

## Temporary Task Files

- Put temporary diagnostic files in `_scratch/<task-id>/`.
- Use a unique task identifier for each directory.
- Keep `/_scratch/` in the repository `.gitignore`.
- Record the directory and its task in the task plan before use.
- Keep files from an interrupted task until that task resumes or its owner authorizes removal.
- Before the task ends, keep specified behavior in tests and necessary fixtures.
- Record permanent decisions in maintained documentation.
- Remove only the temporary directory owned by the completed task.
- Verify that this directory is absent before reporting task completion.
- Preserve directories owned by other tasks.
- If removal fails, report the remaining path and keep task completion pending.

## Dependency Injection In Integration Tests

- Use dependency injection for integration scenarios that are difficult to reproduce.
- Inject the dependency that creates the test condition.
- Use controlled clocks, dependency failures, or unusual responses when the scenario requires them.
- Keep the product logic under test and its related internal interactions real.
- Assert observable results through the public contract.
- Keep integration coverage with real dependency implementations.
- Injected scenarios prove behavior under the specified conditions. They do not prove actual provider connectivity.
- Report actual provider connectivity as unverified. Do not cross the repository boundary to qualify it.

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
- Do not change external application state or external operator state for lifecycle work.
- Do not use another application's source for lifecycle work.
- Use installed lifecycle tools from any location under the repository boundary policy.
- Do not execute publication, deployment, or installation that changes state outside Lo.
- If a lifecycle requirement needs external operations, report that limitation without executing those operations.

## Test-Driven Development

- Start with tests. Define the requested behavior and run its relevant tests before implementation or configuration changes.
- For new behavior, write a test that fails because the behavior is absent. For a defect, write a regression test that reproduces it before the fix.
- Use integration tests as the primary layer for public behavior and component contracts.
- For a bug fix, first reproduce the failure through the relevant public entry point and confirm its cause.
- For a behavior change, add or update coverage that exercises the changed public contract. Choose the smallest meaningful check.
- For a refactor, run relevant existing coverage first. Add characterization coverage where changed behavior is otherwise unprotected.
- Use focused unit tests for algorithms, calculations, and isolated logic when they provide useful evidence.
- Documentation, static configuration, and other low-impact changes can use document, schema, build, or structural checks appropriate to their effect.
- For prose and static configuration, start with existing document, schema, or contract tests and rerun them after the edit. Add a test when the requested behavior lacks coverage.
- Use deterministic local infrastructure for repository-owned databases, filesystems, queues, servers, and browsers.
- At an external provider boundary, use a local protocol implementation with state stored inside Lo.
- Do not use a provider sandbox or live-provider operation for Lo acceptance.

## Validation

Tests provide implementation evidence. Task completion also requires applicable independent reviews, authorization, and selected CI gates.
The repository policy owns CI selection. Global instructions own final-CI timing.

- Use repository-native test targets and the smallest meaningful scope for the changed contract.
- Preserve the failing test and its passing result after the change.
- Report test commands, results, and the behavior covered. Keep untested claims explicit.
- Turn a suspected defect or missing contract into a test.
- Use production observations and diagnostics to inform test design.
- Keep release, publication, deployment, and benchmark execution separate from implementation validation and issue closure.
- Reuse passing results while the tested state remains unchanged, regardless of whether they ran locally or in CI.
- Run full CI when the user explicitly selects it or the repository policy requires it. A repository requirement counts as explicit selection.
- Read any Repository CI Requirements section in this policy when selecting CI. Maintain that local section in `.mprlab/POLICY.REPOSITORY.md` so shared validation rules continue to receive updates.
- Run selected full CI at the final checkpoint after all other authorized work, review findings, acceptance checks, and documentation are complete.
- Use equivalent successful pull-request CI when it supplies all selected checks. Wait for the selected gate before merge or completion.
- When the user prohibits full CI, finish the authorized work with focused checks and report the omitted run. Apply the prohibition to commands, wrappers, delegated work, and actions that trigger CI automatically. If a separate delivery control requires a passing result and none applies, keep its gated delivery pending.
- Use focused checks to correct failures. Repeat selected full CI only at the next final checkpoint after corrections and all other work are complete.
- Run `make verify` only when the user explicitly selects the complete qualification lane.
- For documentation and governance, use the applicable document, schema, and normalizer contract checks and review changed prose.
- For user-visible frontend behavior, use browser tests. For services and CLIs, use HTTP, CLI, or public API tests.
- For read-only work, report source facts and existing test evidence and run only checks necessary for the requested analysis.
- Keep reproduction evidence and any expected failing regression-test result in the task's ignored directory until the task ends.

### Validation Output

- Use tests and necessary fixtures as the permanent record of specified behavior.
- Keep source code, tests, necessary fixtures, configuration, and maintained documentation in Git.
- Keep generated validation reports, logs, and review summaries in the task's ignored `_scratch/<task-id>/` directory.
- Do not commit generated validation output.
- Record completed checks in the task response or pull request description.
- For corrected product defects, keep tests that verify the specified behavior.
- When the task ends, remove temporary validation output under [Temporary Task Files](#temporary-task-files).
- Apply these rules to tools, child processes, and delegated agents.

## Documentation Language

- Use concise, consistent American English for technical prose.
- Preserve facts, product decisions, requirements, conditions, source literals, and ownership boundaries.
- Read `.mprlab/AGENTS.DOCS.md` and repository terminology before technical prose changes.
- Use formal ASD-STE100 compliance only when the user or an applicable document contract explicitly requires it.
- For that selected scope, read `.mprlab/AGENTS.STE.md` and complete its reference and review gates.
- Report the scope and limits of each review. Mechanical checks alone do not prove factual accuracy or formal language compliance.

- For selected formal STE scope, run `prepare-ste-reference --cache-dir <task-directory>/ste-reference --json`.
- Keep reference caches and language-tool output inside Lo's ignored task directory.
- Apply the Lo repository boundary to all documentation tools and child processes.

## Gateway Lifecycle Execution

Use the lifecycle commands and Gateway contract documented by Lo.
Keep release, publication, deployment, and live acceptance as separate results.
Use Gateway v9 conventions only after the repository explicitly adopts that contract.
This guidance refresh does not authorize a lifecycle or manifest migration.
Keep preparation, evidence, and all operation effects inside Lo; external publication, deployment, and installation remain prohibited.

## Manifest Environment Resolution

Use only configuration and credential sources documented by current Lo commands and the selected manifest.
Preserve declared input precedence and keep private values outside public output.
Apply `environment.source` resolution only after the repository explicitly adopts that manifest contract.
This guidance refresh selects no new environment-field shape or credential path.
Keep project inputs, captured state, and validation evidence inside Lo under the repository boundary.

## Native Preparation Authority

Use the native preparation and build commands documented by Lo.
Preserve declared identity, dependency and toolchain requirements, signing inputs, and native command results.
Apply a new shared native-preparation record contract only after explicit repository adoption.
Use simulators, emulators, and automated browsers as sufficient device acceptance environments.
Keep generated output, caches, and evidence inside Lo; adoption does not permit external installation or publication.

## Gateway Credential Inputs

Use manifest-owned credential resolution only when the current selected manifest declares that responsibility.
For other operations, use only credential sources documented by the current Lo command.
Apply the general Credential Discovery gate in `POLICY.md` and the global sign-in preparation rules.
Keep diagnosis and captured task state inside Lo; these rules grant no provider operation or external configuration authority.

## Single Manifest Authority

Follow Lo's documented deployment-specification authority and preserve the current manifest shape.
Keep local orchestration separate from production lifecycle inputs.
Use `.mprlab/deploy/resources.yml` as the sole specification only where Lo has already selected that contract.
A migration to another deployment contract requires a task that explicitly authorizes that change.
Apply the Lo repository boundary throughout; external publication, deployment, installation, and operator-state changes remain prohibited.

## Repository CI Requirements

Implementation changes require final full CI. Reuse equivalent successful pull-request CI for unchanged validated inputs or run `make ci` locally at the selected final checkpoint.
For documentation and governance guidance changes, run the applicable document and Governor checks and review changed prose.
Apply the Lo repository boundary to all checks, review evidence, caches, and child-process output.
