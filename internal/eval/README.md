# LLM evals for MCP servers

`internal/eval` drives a Claude model through the tools of any MCP server and
lets a Go test check what the model did. The cases for `unikraft mcp` live in
`cmd/unikraft/eval`; `task evals` runs them (build tag `evals`) against
staging and needs `ANTHROPIC_API_KEY` plus staging credentials. They cost
money and are not deterministic, so they never gate CI.

## How a conversation works

1. The test connects to the server with an MCP client session; the Unikraft
   cases start the built CLI's `unikraft mcp` over stdio with the test's own
   config and partition (`cmd/unikraft/eval/helpers_test.go`).
2. `eval.New(t, session, cfg, system)` lists the session's tools and hands
   their schemas to the model unchanged.
3. `c.Send(prompt)` sends the prompt and loops: every tool call the model
   makes runs on the server through the session and its result goes back to
   the model, until the model answers. The test fails if the model refuses,
   is cut off, or keeps calling tools for `Config.MaxTurns` turns.
4. Later `Send` calls continue the same conversation, so a test can check the
   outcome of one prompt before sending the next.
5. When the test ends, the conversation is recorded as a row in
   `results.jsonl` and a transcript in `traces/`, graded by whether the test
   passed.

## Writing a case

```go
func TestStop(t *testing.T) {
	env, c := live(t)
	name := newInstance(t, env, "--autostart")
	waitState(t, env, name, "running")

	reply := c.Send("Stop " + name + ".")
	args := decode[controlArgs](t, only(t, reply, "instance_control").Args)
	assert.Equal(t, "stop", args.Action)
	assert.Equal(t, []string{name}, bare(args.Keys...))
	waitState(t, env, name, "stopped")
}
```

A reply has the model's last text and every tool call it made for the prompt:

- `reply.Text` is the answer, `reply.Calls` the calls in order,
  `reply.Tools()` their names and `reply.Called(name)` the calls of one tool.
- `call.Args` is the raw JSON the model passed; decode it into a struct and
  compare with plain testify assertions.
- `call.Result` is the structured content the tool returned, `call.Text` its
  text and `call.IsError` whether the tool reported an error.

Each case is its own test function and runs in parallel with the others.
Repeat the cases with `COUNT=<n>`; the recorder numbers the runs of each test.

## Configuration

`cmd/unikraft/eval` reads its settings from the environment:

| Variable                    | Default             | Meaning                                  |
| --------------------------- | ------------------- | ---------------------------------------- |
| `UNIKRAFT_EVAL_MODEL`       | `claude-sonnet-5-5` | Model under test                         |
| `UNIKRAFT_EVAL_EFFORT`      | API default         | Effort level of the model                |
| `UNIKRAFT_EVAL_CONCURRENCY` | `4`                 | Model requests in flight across tests    |
| `UNIKRAFT_EVAL_TIMEOUT`     | `3m`                | Time limit of one `Send`                 |
| `UNIKRAFT_EVAL_OUT`         | a temp directory    | Where `results.jsonl` and `traces/` land |

```sh
task evals
task evals TEST=TestSandbox COUNT=3
```
