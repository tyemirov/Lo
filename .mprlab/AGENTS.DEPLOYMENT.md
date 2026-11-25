# Deployment And Native Preparation Policy

Read this guide for deployment manifests, release, publication, deployment, or native preparation.
Apply each section only when its stated condition applies. Preserve the selected product and platform contracts.

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

## Static Website Hosting

Apply this section to deployment or publication work for a browser frontend.

- Use GitHub Pages as the production host for each deployable browser frontend.
- Declare the browser frontend in `targets.github_pages.sites[]` in the selected manifest.
- Declare its publication in `publications.github_pages.sites[]`.
- Declare its build output in `artifacts.web_bundles[]`.
- Use `gh-pages` as the publication branch.
- The GitHub Pages repository can differ from the application repository.
- Keep API and service routes on hostnames that differ from the website hostname.
- Reserve the GitHub Pages domain and its `www` hostname for GitHub Pages.
- Treat a container as an artifact source only when its static output goes to GitHub Pages.
- Verify publication through the public website and `/.mprlab-release.json`.
- Run the Governor check after each selected manifest change and before each release, publish, or deploy operation.

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
