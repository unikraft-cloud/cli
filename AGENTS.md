# AGENTS

## Purpose

This repo contains the Go-based Unikraft CLI. Use this file for contributor-facing guidance.

## Development practices

- Use `task` for builds and workflows; enable Remote Taskfiles with `TASK_X_REMOTE_TASKFILES=1`.
- Build the CLI with `task cli`; the binary is placed in `dist/`, and called `unikraft`.
- Build documentation with `task docs`; outputs include Markdown docs in `dist/docs/` and man pages in `dist/man/`.
- Quality gates: `task lint`, `task test`. Run these after making changes to ensure code quality.
- Offline golden tests: `task golden`. These test deterministic output and use `testdata/` golden files. Update them with `task golden-update` - never edit `testdata/` files by hand. Covers `TestHelp` and `TestOutput`.
- Integration tests: `task integration`. These cover end-to-end scenarios against the live API (build tag `integration`). Don't run unless the user prompts you. Integration tests do **not** use golden files; their assertions are inline regex patterns that must be updated in the source code.
- Run these locally before pushing; CI also runs them.

## Architecture overview

- Single CLI binary with a command tree that routes to internal services.
- Core `Model` interface (`internal/model`) models API objects with `Type`, `Key`, `Fields`, and `Raw`, and is extended by get/list/create/edit/delete behaviors.
- Model data is expressed as a tree of `Field` nodes: each node has a name, optional value, nested subfields, and an `Elem` template for homogeneous arrays; patch metadata and verbosity drive editing and output.
- Model utilities live alongside the model layer (inside `internal/model`): struct↔field conversion, field cloning/merging/deduping, map/slice encoding, path-based filtering/selection, and patch helpers for edits.
- Core logic lives under `internal/` and is organized by domain: configuration/profile handling, API client communication, model abstractions, output/logging utilities, multi-region support, and shared helpers.
- Build artifacts and generated docs go to `dist/`; developer tooling/scripts live at the repo root and under `tools/`.

## Important dependencies

- `kong` for CLI parsing and command/flag wiring
- `unikraft.com/cloud/sdk` for Unikraft Cloud API clients and types.
- `unikraft.com/x/...` repos for Unikraft-specific helpers (logging, colors, pointer utilities, terminal sizing, etc.).

## Debugging TUI issues

If you need to debug or test TUI behavior, use the `testtui` tool in `tools/testtui/`:

```sh
go run ./tools/testtui [resource-type] [resource-key]
```

This tool runs the same TUI model as `unikraft tui` but accepts scripted commands via stdin instead of taking over the terminal. You can:

- Send keystrokes: `key enter`, `key ?`, `key ctrl+c`
- Wait for content: `wait contains("Home")`, `wait not contains("Loading...")`
- Capture snapshots: `snapshot` (prints current rendered view)
- Add delays: `sleep 150ms`

Example workflow to reproduce TUI states:

```sh
cat <<'EOF' | go run ./tools/testtui instances
wait not contains("Loading...")
snapshot
key enter
wait not contains("Loading...")
snapshot
EOF
```

Use `--output=json` for machine-readable events including snapshots and errors. See `tools/testtui/README.md` for full documentation including wait expressions, named pipes for long-lived sessions, and JSON output format.

## Advice

- If modifying existing functionality that has tests, add new tests to cover the changes in behavior.
- If wanting to explore Go documentation or inspect Go code, use the `go doc` command - don't explore the filesystem outside of the current directory for this.
- For terminal/TUI color styling, use `unikraft.com/x/colors` tokens instead of arbitrary hard-coded color values.
- If writing multi-line strings, use backticked strings.
- Use the newest Go features you know about; do not worry about old Go versions. For example:
  - Use `errors.Join` for combining errors.
  - Use the `slices`/`maps`/`cmp`/`iter` packages for common operations on slices/maps/comparisons/iteration.
  - Use `for i := range <n>` for iterating a fixed number of times.
  - No need to use `i := i` in loops to capture loop variables for closures; Go 1.21+ captures them by default.
  - Prefer `new(value)` to get pointers to literals now that Go 1.26 allows expressions in `new`.
  - No need to check for the usage of `new(value)` like `new(1)`; Go 1.26+ allows it and is more concise than `ptr.To(1)`.
  - Use the `testify` package when writing tests like in the current tests.
