# Contributing

## Prerequisites

- [Go](https://golang.org/dl/) 1.25+
- Git
- [Task](https://taskfile.dev/) (or GNU Make, which wraps Task transparently)

Enable Remote Taskfiles:

```sh
export TASK_X_REMOTE_TASKFILES=1
```

## Building

Build the CLI binary:

```sh
task cli
```

The binary is placed at `dist/unikraft`.

To generate documentation (Markdown docs and man pages):

```sh
# Generate all docs
task docs

# Generate man pages only
task docs:man

# Generate markdown docs only
task docs:mdx
```

Output is placed in `dist/docs/` and `dist/man/`.

## Running tests

Run unit tests:

```sh
task test
```

Run offline golden (snapshot) tests:

```sh
task golden
```

If golden test expectations change, update them:

```sh
task golden-update
```

Never edit files in `testdata/` manually — always use `task golden-update`
to regenerate them.

Run integration tests (requires cloud credentials):

```sh
task integration
```

Run the linter:

```sh
task lint
```

Run tests and linting locally before pushing; CI also runs them.

## Releasing

Releases are performed by merging into the `prod-stable` branch and pushing it. CI
automatically determines the git tag — no manual tagging is needed.

## Architecture

The CLI follows a resource-oriented architecture:

- **Commands** (`internal/cmd/`) — Kong-based command definitions with subcommand routing
- **Resources** (`internal/resource/`) — Unified interface for API objects with field introspection
- **Multi-Metro** (`internal/multimetro/`) — Client abstraction for global infrastructure operations
- **Configuration** (`internal/config/`) — Profile and credential management
- **Telemetry** (`internal/telemetry/`) — Usage analytics (opt-out via `--no-telemetry`)

### Key Dependencies

- [Kong](https://github.com/alecthomas/kong) — CLI parsing and command wiring
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — Terminal UI components
- [Unikraft Cloud SDK](https://unikraft.com/cloud/sdk) — API client library
