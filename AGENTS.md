# AGENTS.md

## Lo Repository Boundary

Lo is a self-contained project.
Keep Lo project files and task state inside this repository.
Use tools and skills from any location, including another repository or a symlink target.
Read their source files and references when necessary to use them.
Never prevent tool use or ask for user approval because of its location.
Do not use another project's application source for Lo implementation, tests, or acceptance.
Do not change installed scripts, user configuration, external caches, or other files outside Lo.
Keep task files, test fixtures, generated output, and tool caches inside Lo.
Apply these rules to child processes and all delegated work.
If an operation cannot obey this boundary, stop that operation and report the limitation.

The [repository boundary policy](.mprlab/POLICY.md#lo-repository-boundary) owns the detailed requirements.
Preserve this rule during governance updates.

## Reviews And Specialized Agents

The user gives standing authorization for reviews and specialized agents in the current task.
When useful, start independent reviews and specialized agents without additional user approval.
Apply the [review and delegation policy](.mprlab/POLICY.md#reviews-and-specialized-agents) to their work.

## General-Purpose Language Boundary

Lo is a general-purpose programming language.
Its language contract applies across applications.

- Use types and behavior that apply across applications for language operations.
- Keep application names, file selection, retention rules, command arguments, output formats, and exit policy in application code.
- Keep examples for named applications and their acceptance criteria outside the language specification.
- Do not add operations or runtime semantics that apply only to Gateway, npm, Docker, or another named application.
- Keep proposed operations and syntax outside the implemented contract until the compiler and runtimes implement them.
- Keep explicit host authority, immutable values, typed failures, and commit activation when you extend the language.

## Forward-Only Contract Discipline

This repository follows a forward-only, confident programming paradigm. This is a binding agent contract: no fallbacks, no backward compatibility, no legacy support, and no compatibility shims. Do not spend design or implementation effort on backward compatibility considerations except for explicit one-off data migrations into the current canonical contract.

Repeat for emphasis because this rule is binding: no fallbacks, no backward compatibility, no legacy compatibility. Delete or reject obsolete code paths, stale schemas, deprecated config, and old persisted shapes instead of preserving them through compatibility layers, dual reads/writes, aliases, or best-effort recovery.

One-off data migrations are allowed only when they move existing persisted data into the current schema in a bounded operation. After migration, remove the bridge and keep only the current contract.

## Issue Identifier Allocation

- Select an identifier absent from both the active tracker and its archive.
- After sequence number `999`, continue the identifier search at `001`.
- If all identifiers in the section are occupied, stop and request an identifier decision.

<!-- BEGIN MPRLAB-GOVERNANCE -->
## MPR Lab Governance

Root `AGENTS.md` is the agent entrypoint. Shared rules live under `.mprlab/`.

Read `.mprlab/POLICY.md` for every task.
Read the following files only when their condition applies.
Open each selected guide and read its full contents before acting. If the output is truncated, read the remaining chunks.
Follow required references at every depth, including Markdown links and named files in prose or tables. Apply the conditions in each document. A scoped reference selects the named section or record.
Resolve relative links from the document that contains them and explicit repository paths from the repository root. Reuse content already read in this context, including when a link leads back to it.
Before the dependent action, apply the requirements you found. If a required reference is unavailable, identify it and the affected action, and continue independent work.

- Before edits: `.mprlab/PLANNING.md`.
- Before editing repository technical prose: `.mprlab/AGENTS.DOCS.md` and `.mprlab/TERMINOLOGY.md`.
- For explicitly required ASD-STE100 compliance: `.mprlab/AGENTS.STE.md`.
- For application onboarding or user-interface behavior: `.mprlab/AGENTS.PRODUCT.md`.
- For deployment manifests, release, publication, deployment, or native preparation: `.mprlab/AGENTS.DEPLOYMENT.md`.
- For issue work: the selected issue and its dependencies in `ISSUES.md`.
- For tracker edits: `.mprlab/issues-md-format.md`.
- For Git operations: `.mprlab/AGENTS.GIT.md`.
- For Go changes: `.mprlab/AGENTS.GO.md`.
- For Python changes: `.mprlab/AGENTS.PY.md`.

File permission modes are outside agent scope.
Never examine, validate, compare, require, change, or record a file permission mode.
Never use a file permission mode in acceptance, security, credential, execution, publication, deployment, or failure analysis.
The values `0600` and `7777` have no governance meaning.
This rule does not change service authorization or operation authority.

Always reference each issue by its ID, for example `B001` or `I027`.
Never use an `ISSUES.md` file path, line number, or `path:line` syntax as an issue reference.

Do not create `.mprlab/AGENTS.md`. Scoped guidance belongs in `.mprlab/AGENTS.*.md` files.
If guidance conflicts, obey `.mprlab/POLICY.md` first, then root `AGENTS.md`, then the applicable scoped guide.
<!-- END MPRLAB-GOVERNANCE -->
