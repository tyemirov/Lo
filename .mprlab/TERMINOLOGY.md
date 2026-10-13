# Repository Terminology

This file contains the approved technical nouns and technical verbs for repository documentation.

Use this file with `.mprlab/AGENTS.DOCS.md` and ASD-STE100 Simplified Technical English, Issue 9.

Do not add a general dictionary word to this file. Use the ASD-STE100 dictionary for general words.

Give each term one meaning. Use the same term for the same concept in all documents.

## MPR Lab Technical Nouns

- `acceptance criteria`: Conditions that show that a change has the necessary behavior.
- `active issue tracker`: The canonical file that contains current work.
- `ADR`: An architecture decision record.
- `adapter`: A code unit that connects a core module to an external system.
- `agent guide`: A file that gives binding instructions to an agent.
- `API`: A repository-owned application programming interface.
- `API contract`: The canonical schema and behavior of an API.
- `ASD-STE100`: The Simplified Technical English standard for technical documentation.
- `architecture`: The structure, boundaries, and ownership of a software system.
- `App Store Connect`: The Apple service that receives and manages iOS store artifacts.
- `artifact`: A file or image that a build, release, or generator creates.
- `backlog`: The set of unresolved issues in the active issue tracker.
- `backend client`: A code unit that sends requests to a backend.
- `browser frontend`: A user interface that operates in a web browser.
- `build`: A process or output that converts source code into an artifact.
- `changelog`: A file that records completed changes for releases.
- `characterization test`: An integration test that records current public behavior before a refactor.
- `CI`: The repository continuous-integration system.
- `CLI`: A command-line interface.
- `code path`: A sequence of operations in source code.
- `config`: Source-controlled configuration data.
- `container`: An isolated runtime package with an application and its dependencies.
- `contract`: A binding definition of behavior, data, or ownership.
- `credential`: A private value that an external service uses to authenticate an identity.
- `documentation`: Technical information in repository documents.
- `coverage`: Evidence that tests exercise specified behavior.
- `dependency`: An external or internal component that a system requires.
- `dependency injection`: A design that supplies a component's dependencies from outside that component.
- `deployment`: An operation that changes a runtime environment.
- `domain type`: A type that represents validated domain data.
- `EAS`: Expo Application Services for hosted build, submission, and update operations.
- `endpoint`: One HTTP API address and its operation.
- `end user`: The person who requests or receives the agent work.
- `environment file`: A private file that contains environment variable assignments.
- `environment variable`: A named process input.
- `Expo`: A framework and source config system for React Native mobile clients.
- `Expo CLI`: The Expo command-line tool for local development and native project generation.
- `file permission mode`: A number or symbol that gives filesystem access bits.
- `Google Play`: The Google service that receives and manages Android store artifacts.
- `GitHub Pages`: The GitHub service that hosts a static website from a repository branch.
- `issue`: One tracked unit of work.
- `issue tracker`: A file or system that contains issues.
- `integration test`: A test of real product logic and component interactions through a public entry point, with controlled dependencies when necessary.
- `inverted test pyramid`: The MPR Lab test strategy with integration tests as the primary layer and focused unit tests where useful.
- `language checker`: A tool that finds specified language errors.
- `language review`: An agent-owned examination of text against language rules and terminology.
- `manifest`: A source-controlled file that declares resources or configuration.
- `mobile client`: An application for a mobile platform.
- `mobile store artifact`: A signed `.ipa` or `.aab` file for store publication.
- `native toolchain`: The platform tools that build and sign a mobile store artifact.
- `payload`: Structured data that crosses a system boundary.
- `PDF`: A file that uses the Portable Document Format.
- `PRD`: A product requirement document.
- `private input channel`: A documented process environment, anonymous pipe, or private file input.
- `production code`: Source code that implements repository behavior outside the test suite.
- `producing agent`: The agent that creates or changes technical prose.
- `public entry point`: An interface through which a user or caller uses repository behavior.
- `pull request`: A proposed Git change for review and merge.
- `repository`: A source-controlled project and its files.
- `reference cache`: A private local directory that stores a verified official reference.
- `route`: An API or user-interface address and its handler.
- `runbook`: A technical procedure for an operator or agent.
- `runtime`: An operating instance of a service or application.
- `schema`: A machine-readable definition of structured data.
- `SHA-256`: A cryptographic digest that identifies the verified official reference.
- `source code`: Human-readable instructions that define software behavior.
- `source blocker`: A failure that prevents access to a necessary official source.
- `stack guide`: An agent guide for one language, framework, or runtime.
- `STE reference`: The verified official ASD-STE100 PDF that controls a language review.
- `store publisher`: A repository-owned tool that submits a mobile store artifact directly to its store.
- `static website`: A browser frontend that uses generated files without a website server runtime.
- `technical document`: A repository document that contains technical information or instructions.
- `technical noun`: A subject-field noun that the repository approves.
- `technical prose`: English technical text outside code and source-controlled literals.
- `technical verb`: A subject-field verb that the repository approves.
- `test-driven development`: A coding sequence that uses a failing integration test before a production code change.
- `unit test`: A test that isolates one code unit from its collaborators.
- `validation`: Evidence that a change obeys its current contract.
- `worktree`: A Git checkout that has its own working directory.
- `website hostname`: The hostname that identifies a public static website.

## Repository Technical Nouns

- `listener`: A network resource that accepts connections at one local address and port.
- `loopback address`: A network address for connections within the same host.
- `readiness event`: A CLI result that identifies the servers active after program startup.
- `shutdown`: The runtime sequence that stops requests and closes active resources.
- `startup`: The runtime sequence that evaluates the program before the service readiness event.
- `timeout`: A specified duration after which the host cancels an operation.
- `Base64`: A text encoding for a sequence of bytes.
- `directory root`: An opened directory that limits adapter path access to its directory tree.
- `execution budget`: A host limit on interpreter steps or evaluated value data.
- `quotient`: The integer result of division after removal of the fractional part toward zero.
- `remainder`: The integer difference between a dividend and the product of its divisor and quotient.
- `symlink`: A filesystem entry that refers to another path.
- `value weight`: The specified byte count for expanded runtime data and its fixed value overhead.
- `activation`: Execution of a committable target through a Lo `commit` statement.
- `adapter boundary`: The interface between Lo execution and an external resource.
- `arbitrary-precision integer`: An integer whose representation can increase with its magnitude.
- `argument`: A value or flow reference supplied to a Lo operation.
- `AST`: An abstract syntax tree that represents parsed Lo source code.
- `atomic type`: A built-in Lo type with no component type parameters.
- `binding`: An immutable association between a Lo name and a value.
- `boundary read`: An operation that obtains data from an external resource during Lo execution.
- `built-in catalog`: The canonical compiler definitions for Lo types and operations.
- `byte offset`: A position measured in bytes from the start of a source file.
- `call graph`: A graph that connects flows to the flows that they invoke.
- `callback`: A named flow that an operation invokes through a flow reference.
- `capability`: Host authorization for a specified operation on an external resource.
- `checked graph`: An execution graph that satisfies compiler type, reference, and control invariants.
- `committable target`: An immutable activation description accepted by a Lo `commit` statement.
- `compiler`: The program that converts Lo source code into a checked execution graph.
- `conformance fixture`: A source program and expected result for a specified language rule.
- `constructor`: An operation that creates a typed value.
- `control dependency`: A graph relation that restricts when a node can execute.
- `data dependency`: A graph relation that supplies a value to another node.
- `declaration`: A source construct that introduces a name and its meaning.
- `diagnostic`: A structured report of a source or execution failure.
- `effect summary`: Static information about possible activation through a flow or operation.
- `evaluation order`: The sequence in which Lo evaluates expressions and activates targets.
- `evaluation region`: Graph nodes that share a branch, invocation, or handler boundary.
- `exact record`: A record type that requires a specified set of fields and field types.
- `execution graph`: The typed representation that defines Lo operations and their dependencies.
- `fallibility`: The property that an expression can produce a recoverable Lo error.
- `flow`: A named Lo graph template with typed parameters and an output type.
- `flow reference`: A resolved name that identifies a flow for invocation by an operation.
- `flow signature`: The declared parameter types, output type, and handler relation of a flow.
- `frontend`: Compiler stages that parse source code and establish its semantic meaning.
- `handler boundary`: A flow or adapter boundary that receives a propagated Lo error.
- `host exception`: A failure mechanism of the implementation language outside the Lo value model.
- `immutable value`: A value whose contents cannot change after construction.
- `invariant`: A property that all values of an internal compiler representation must satisfy.
- `inspection report`: Static information about the behavior and resource requirements of a checked Lo program.
- `interpreter`: The component that evaluates a checked Lo execution graph.
- `IR`: The intermediate representation of a Lo program as a typed execution graph.
- `lexer`: The compiler component that converts source bytes into tokens.
- `lexical scope`: The source region that determines which declarations a name can reference.
- `literal`: Source syntax that directly denotes a value.
- `Lo error`: A recoverable failure represented by the built-in Lo `Error` value.
- `nominal type`: A type whose identity depends on its declared name.
- `operation identifier`: The internal identity of one operation in the built-in catalog.
- `node`: One operation or value in the execution graph.
- `parser`: The compiler component that converts tokens into an AST.
- `pipe`: The Lo `|>` expression that supplies its left value as the first call argument.
- `processor`: A built-in Lo operation that transforms values.
- `projection`: An expression that obtains a named field from a typed value.
- `pure flow`: A Lo flow classified as free of commit activation under the language purity rules.
- `recursion`: Invocation that returns to an active flow through a cycle of flow calls.
- `resolver`: The compiler component that connects references to declarations.
- `scalar`: One Unicode scalar value.
- `source span`: A source file identifier and the byte offsets of a source construct.
- `statement`: A Lo binding, flow definition, commit, or return in a source body.
- `structural type`: A type whose identity depends on its members and their types.
- `symbol`: The compiler identity of a declaration in a lexical scope.
- `token`: A lexical unit with a kind, source text, and source span.
- `trace frame`: Flow and operation context attached to a propagated Lo error.
- `type checker`: The compiler component that enforces Lo type and fallibility rules.
- `type unification`: The derivation of a common type under the Lo type rules.
- `verifier`: The compiler frontend that determines whether a Lo program satisfies the language contract.

## MPR Lab Technical Verbs

- `archive`: Move completed history from the active issue tracker to durable storage.
- `authenticate`: Confirm the identity of a client or user.
- `authorize`: Confirm that an identity can do an operation on a resource.
- `build`: Convert source code into an executable or generated artifact.
- `cache`: Store a verified reference outside a target repository for repeated use.
- `commit`: Record a Git change in repository history.
- `configure`: Set source-controlled values that control system behavior.
- `deploy`: Change a runtime environment to use a specified artifact and configuration.
- `file`: Add an issue to the active issue tracker.
- `generate`: Create an artifact from its canonical source.
- `lint`: Use static rules to find source or document errors.
- `merge`: Add the changes from a pull request to its target branch.
- `normalize`: Change a file to obey one canonical format or contract.
- `parse`: Convert input data into a typed internal value.
- `publish`: Make an artifact available outside the source repository.
- `refactor`: Change code structure without a change to public behavior.
- `regenerate`: Create a generated artifact again from its canonical source.
- `redistribute`: Provide a third-party reference outside its approved distribution method.
- `render`: Convert source data into a visible or machine-readable output.
- `retrieve`: Get an official reference from its approved source.
- `review`: Examine an artifact against its requirements and record the result.
- `scan`: Use an automated process to find specified source patterns.
- `serialize`: Convert a typed value into a transport or storage representation.
- `validate`: Confirm that an input or artifact obeys its contract.
- `verify`: Confirm a result at its public or runtime boundary.

Use the simple present, simple past, simple future, imperative, or infinitive form of these verbs.

## Repository Technical Verbs

- `compile`: Convert Lo source code into a checked execution graph. Forms: compile, compiles, compiled.
- `construct`: Make a typed Lo value or compiler representation. Forms: construct, constructs, constructed.
- `dispatch`: Invoke the configured Lo handler for an adapter request. Forms: dispatch, dispatches, dispatched.
- `evaluate`: Execute a Lo expression or graph region to obtain its result. Forms: evaluate, evaluates, evaluated.
- `execute`: Run a checked Lo program or operation. Forms: execute, executes, executed.
- `implement`: Add source code for a specified compiler behavior. Forms: implement, implements, implemented.
- `infer`: Derive a Lo type from expressions and their expected types. Forms: infer, infers, inferred.
- `invoke`: Start one evaluation of a named Lo flow. Forms: invoke, invokes, invoked.
- `lower`: Convert a checked source construct into explicit graph operations. Forms: lower, lowers, lowered.
- `propagate`: Transfer a Lo error or semantic property across graph boundaries. Forms: propagate, propagates, propagated.
- `resolve`: Associate a Lo name with its declaration. Forms: resolve, resolves, resolved.
- `return`: Transfer a result from a compiler operation or a Lo flow to its caller. Forms: return, returns, returned.
- `unify`: Derive a common Lo type under the language rules. Forms: unify, unifies, unified.
