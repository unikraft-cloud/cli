# LLM evals for the MCP tool surface

`internal/eval` drives a Claude model through the tools of `unikraft mcp` and
lets a Go test assert on the tool calls the model makes. The cases live in
`cmd/unikraft/eval`; `task eval` runs them (build tag `eval`) and needs
`ANTHROPIC_API_KEY`, plus staging credentials for the live cases. They cost
money and are not deterministic, so they never gate CI.

## How a run works

1. `mcptest.Connect` starts the built CLI's `unikraft mcp` over stdio and
   keeps the tools and instructions it announces
   (`cmd/unikraft/eval/helpers_test.go`).
2. `ToolParams` hands the tool schemas to the model unchanged.
3. `Agent.Run` sends the prompt and loops: every `tool_use` block becomes a
   `Step`, `Exec` runs it and its result goes back to the model, until the
   model answers, refuses, is truncated or `Config.MaxTurns` is reached.
   `Exec` may return `ErrStop` to end the run at a call without executing it.
4. `Runner.Prompt` wraps a run as a parallel subtest, repeats it
   `Config.Reps` times, requires `StatusOK`, and records a row in
   `results.jsonl` and the transcript in `traces/` under `Config.OutDir`.

Two executors exist in `cmd/unikraft/eval`:

- selection (`stub`): read-only tools get canned answers and the run stops at
  the first call that would change something, so no backend is needed and a
  model may look before it acts. `TestToolSelection` judges read-only prompts
  by the first call and mutating prompts by the last.
- live (`Session.Call`): every call runs against staging through the real
  server, in a partition torn down after the test; `TestLive` then inspects
  the outcome with the CLI.

## Writing a case

```go
r.Prompt(t, "stop-one", "Stop web-1.", func(t *testing.T, res *eval.Result) {
	call := res.Last(t)
	assert.Equal(t, "instance_control", call.Tool)
	call.Expect(t, eval.Args{"action": "stop", "keys": eval.Keys{"web-1"}})
})
```

What a result offers:

- `res.Steps` is every call in order and `res.Tools()` their names.
  `assert.Equal(t, []string{"instance_logs"}, res.Tools())` pins the whole
  sequence, `assert.Subset(t, []string{"instance_create", "instance_list"},
  res.Tools())` allows only those tools, and
  `assert.Len(t, res.Calls("instance_create"), 1)` how often one was called.
- `res.First(t)`, `res.Last(t)` and `res.Call(t, tool)` give one call and
  fail the test when it is missing; `res.Calls(tool)` gives every call of a
  tool.
- `step.Args` is the raw JSON, `step.Arg(path)` a gjson lookup,
  `step.Arguments()` the decoded map for `assert.Subset` and friends, and
  `step.Result` with `step.IsError` what the executor returned.
- `res.Text` is the model's last reply, `res.Status` one of `ok`,
  `truncated`, `refusal` or `max_turns`, `res.Usage` the token counts.

`Expect` checks each path of an `Args` map against the call: a string, bool
or number compared as text, a `[]string` compared as a set, `Keys` compared
as a set of resource names with any `metro/` prefix dropped, a
`*regexp.Regexp` matched against the raw JSON, or `Present` and `Absent`.
Dotted field paths go through `Path("set", "resources.memory")`.

`r.Conversation(t, name, []string{...}, check)` sends several prompts in
turn, each once the model has answered the previous one; the result covers
the whole conversation.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `UNIKRAFT_EVAL_MODEL` | `claude-sonnet-5-5` | model under test |
| `UNIKRAFT_EVAL_REPS` | 1 | runs per prompt |
| `UNIKRAFT_EVAL_OUT` | a temporary directory | where `results.jsonl` and `traces/` land |
| `UNIKRAFT_EVAL_EFFORT` | the API default | effort level |
| `UNIKRAFT_EVAL_CONCURRENCY` | 4 | model requests in flight |
| `UNIKRAFT_EVAL_TIMEOUT` | 3m | limit for one run of a prompt |

`task eval TEST=TestToolSelection` runs one suite.
