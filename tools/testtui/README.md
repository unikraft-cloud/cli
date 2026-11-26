# testtui

`testtui` is a small, scriptable driver for the Unikraft Bubble Tea TUI.

It runs the same model as `unikraft tui`, but in a terminal emulator
(`unikraft.com/x/integration/tuikit`) instead of taking over your terminal. It:

- accepts a stream of simple commands (`key`, `sleep`, `wait`, `snapshot`)
- executes them against the live TUI, as key presses in that terminal
- prints the screen only when you ask for a `snapshot` (text mode)
- can emit an ack event after every command (`--output=json`)

This is useful for debugging TUI behavior, reproducing tricky UI states, and
writing/iterating on golden snapshot tests.

## Run

From the repo root:

```sh
go run ./tools/testtui
```

`testtui` uses the same configuration as the main CLI. If you haven't logged in
yet, resource panels will show the usual `profile not setup` error.

```sh
unikraft login
```

You can optionally start directly on a resource list or detail view:

```sh
# Home
go run ./tools/testtui

# Resource list (either singular or plural works)
go run ./tools/testtui instances

# Resource detail
go run ./tools/testtui instances fra/my-instance
```

By default it reads commands from stdin (`--script -`).

Useful flags:

- `--config` (or `UNIKRAFT_CONFIG`): path to the config file
- `--profile` (or `UNIKRAFT_PROFILE`): select the active profile
- `--output`: `text` (default) or `json`
- `--width`, `--height`: snapshot dimensions (lines are truncated to fit)
- `--wait-timeout`: default timeout for `wait ...`
- `--script <path>`: read commands from a file (use `-` for stdin)
- `--cmd <line>`: run a command line before the script (repeatable)

## Language

Each line is one command: `key`, `wait`, `sleep` or `snapshot`. The language,
the wait expressions, the keys and the JSON events are documented with the
code, in
[`x/integration/tuikit/script`](https://github.com/unikraft-cloud/x/tree/prod-staging/integration/tuikit/script).

## Examples

Show the home screen:

```sh
cat <<'EOF' | go run ./tools/testtui
snapshot
EOF
```

Same, but JSON output (extract only snapshots):

```sh
cat <<'EOF' | go run ./tools/testtui --output=json | jq -r 'select(.snapshot != null) | .snapshot'
snapshot
EOF
```

Start on the instances list and snapshot once it's settled:

```sh
cat <<'EOF' | go run ./tools/testtui instances
wait not contains("Loading...")
snapshot
EOF
```

Toggle help. A `key` returns before the screen has changed, so `wait` for
what it puts there before a `snapshot`:

```sh
cat <<'EOF' | go run ./tools/testtui
snapshot
key ?
wait not contains("• ? help")   # the short help has gone
snapshot
EOF
```

## Long-lived sessions (named pipe)

For agent-style workflows it's useful to keep `testtui` running and send it
commands over time.

You can do that with a FIFO (named pipe), but you must keep a writer open.
If the last writer closes, `testtui` will read EOF and exit.

Two-terminal setup:

Terminal A:

```sh
mkfifo /tmp/testtui.pipe
go run ./tools/testtui --output=json --script /tmp/testtui.pipe --width 80 --height 24 \
  | tee /tmp/testtui.jsonl
```

To see only snapshots in a human-readable form:

```sh
tail -f /tmp/testtui.jsonl | jq -r 'select(.snapshot != null) | .snapshot'
```

Terminal B (keep a writer open on FD 3):

```sh
exec 3>/tmp/testtui.pipe

printf 'snapshot\n' >&3
printf 'key ?\nwait not contains("• ? help")\nsnapshot\n' >&3
```

If you need to send one-off commands from many short-lived processes (common for
agents), keep the pipe open with a dedicated "keeper" process:

```sh
# Keep the FIFO open (no output is written).
tail -f /dev/null > /tmp/testtui.pipe &

# Now you can send commands without keeping an FD open.
printf 'snapshot\n' > /tmp/testtui.pipe
printf 'key ?\nwait not contains("• ? help")\nsnapshot\n' > /tmp/testtui.pipe
```

To stop the tool you can either kill it, or send `key ctrl+c` (the TUI quit
binding).
