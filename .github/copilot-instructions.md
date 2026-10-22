# Copilot review instructions

## User-facing changes and integration-test coverage

When reviewing a change, first determine whether it adds or changes user-facing CLI functionality. Treat the following as user-facing changes:

- New, removed, or changed commands and subcommands.
- New, removed, or changed positional arguments, flags, options, aliases, defaults, or validation.
- Changes to command behavior, output, exit status, authentication, configuration, resource workflows, or other documented CLI interactions.

For every user-facing change, check whether the change has appropriate integration-test coverage. Repository integration tests live in:
- `cmd/unikraft/integration/` — end-to-end CLI tests, organised as one file per command area, with shared fixtures in `helpers_test.go`.
- `internal/cmd/build_integration_test.go` — integration-level builder tests that exercise real builds through `unikraft.com/x/build` rather than the CLI surface.

Both use the shared runner/helpers in the `internal/integration/` package; that package holds the harness itself, not test cases. All of them run through `task integration`, which targets `./cmd/unikraft/integration` and `./internal/cmd`.

If integration tests are missing or do not cover the changed user-facing behavior, leave one **medium-level** review comment. The comment must:

1. State that integration coverage is missing or incomplete.
2. Ask the author to add the tests.
3. Give a concrete checklist of behavior to cover, tailored to the change. Include relevant cases such as:
   - Successful invocation using the new or changed command, argument, flag, or option.
   - The resulting observable behavior: output, resource state, API-side effect, or exit status.
   - Invalid, conflicting, omitted, or boundary input and the expected error/validation behavior, when applicable.
   - Relevant interactions with existing commands, output formats, config/profile selection, or lifecycle flows, when applicable.
   - Cleanup of resources created by the test, when applicable.

Keep the comment focused on the missing integration coverage. Do not request integration tests for changes that do not alter user-facing behavior, or when existing tests already adequately cover the change. In those cases, do not leave a comment about integration tests.

Do not treat unit, golden, or output tests alone as a substitute for integration coverage when the changed behavior is an end-to-end CLI interaction.
