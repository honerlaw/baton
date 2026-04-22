# AGENTS.md

Orientation for AI coding agents working in this repo. Read this before
making changes.

## What baton is

A Go CLI + TUI that runs a multi-stage agentic workflow against a codebase.
The default pipeline is seven stages (context → design → parallel design
review → design synthesis → implementation → parallel code review →
verdict) but the **runtime is workflow-agnostic**: workflows are YAML
files parsed into data. There is no hardcoded notion of "design stage"
or "review stage" in the orchestrator. If you add a constant like
`SevenStages` or a `switch` on stage purpose, you are off-track.

## Repo layout

```
cmd/baton/              cobra entrypoint
internal/orchestrator/  workflow engine (sequential, parallel, verdict re-entry)
internal/workflow/      YAML schema, loader, validator, template resolver
internal/persona/       Claude-format parser + chained loader (precedence)
internal/agent/         single-stage streaming agent loop
internal/openrouter/    streaming SSE client (stdlib net/http)
internal/tools/         Tool interface, Registry, built-in tools
internal/event/         typed events + fan-out bus with lossy subscribers
internal/artifact/      run-dir layout, ULID run IDs, re-entry archiving
internal/tui/           Bubble Tea TUI
internal/render/        plaintext + NDJSON event consumers
internal/config/        XDG config + env-var merge
internal/assets/        //go:embed of default personas + workflows, scaffolder
internal/cli/           cobra subcommand wiring

internal/agents/  internal/logx/  internal/runtime/
    Retained package-continuity stubs from a prior layout. Do NOT revive
    them or add code there — they exist only so the tree stays consistent
    with its history. The real packages are agent/, event/, render/.
```

## Core invariants

1. **Workflows are data.** The orchestrator walks `[]workflow.Stage`.
   Stage IDs are the only way to reference a stage (re-entry, task
   ordering). Artifact names are the only way to reference outputs.
   Parallel members are not accessible by ID from outside their group.

2. **Artifact names are globally unique per workflow**, including
   across parallel group members. The validator enforces this.

3. **Inputs come from *prior* stages only.** Parallel members cannot
   see each other's outputs — that's the point of parallel review.

4. **Model precedence:** persona frontmatter > stage `model:` > workflow
   `default_model:` > `Orchestrator.DefaultModel` (tool-level).

5. **Personas are load-bearing.** The shipped personas in
   `internal/assets/personas/` follow a specific behavior-first style
   (how-you-think / required-output / checklist / escalation / scope).
   Changing a persona can meaningfully change output quality. Treat
   edits with the same seriousness as changing a prompt that runs in
   production (because it does).

6. **`write_file` must land inside the working directory or the run
   directory.** This is enforced in `internal/tools/paths.go`. Do not
   relax it without a good reason; it's the only guardrail on tool
   output.

7. **Streaming tool calls are assembled by index.** OpenRouter delivers
   `function.name` once and `function.arguments` in fragments across
   many chunks with the same `index`. See
   `internal/openrouter/client.go#parseSSE` and
   `internal/agent/agent.go#finalizeToolCalls`. If you edit either, keep
   the other consistent.

8. **Event bus backpressure is asymmetric.** `MessageStreamed` deltas
   are droppable under load for lossy subscribers (TUI). All other
   event types block-and-send. Preserve that distinction if you add
   event types.

9. **Re-entry preserves prior outputs.** Stage re-entry moves
   artifacts to `artifacts.prev-N/` — it does not overwrite. Retrying
   code should read from `{{ .prev.<name> }}` in templates if it needs
   the old output.

## Commands agents run often

The Makefile is a thin wrapper — each target is a single `go …` or
`golangci-lint …` invocation. CI runs the same commands directly.

```sh
make build        # go build -trimpath -o baton ./cmd/baton
make test         # go test -race -count=1 ./...
make vet
make lint         # golangci-lint run  (install with `make tools`)
make fmt          # gofumpt -w . && goimports -local … -w .
make fmt-check    # fails if anything would be reformatted
make validate     # baton validate for each shipped workflow
make ci           # vet + test + lint (what CI runs)
make tools        # install golangci-lint at the pinned version
```

`go vet`, `go test -race`, and `golangci-lint run` all must stay green
— they're gated on every PR by `.github/workflows/ci.yml`.

For live-LLM testing, set `OPENROUTER_API_KEY` and run against a toy
feature request. The orchestrator is deterministic-enough that a mock
LLM (see `internal/orchestrator/orchestrator_test.go`) is usually
sufficient.

## Linting

Configured in `.golangci.yml`. Enabled: `govet`, `staticcheck`,
`errcheck`, `unused`, `ineffassign`, `gofumpt`, `goimports`, `revive`,
`bodyclose`, `errorlint`, `misspell`, `unconvert`, `nolintlint`.

Known-conservative choices:
- `errcheck` exempts `Close`, `fmt.Fprint*`. Intentional elsewhere:
  use `_ = f()` or `defer func() { _ = f() }()`, not `//nolint`.
- `revive`'s `exported` rule is disabled (too noisy for pkg-internal
  helpers). Doc exported types anyway — it's the style.
- `revive`'s `unused-parameter` is disabled.
- Test files get a pass on `errcheck` and `revive`.

Don't add `//nolint` directives without a one-line comment explaining
why. `nolintlint` enforces that.

## Testing patterns

- **Fake LLM**: implement the `agent.LLM` interface, return a closed
  channel of `openrouter.StreamFrame` values. Examples:
  `internal/agent/agent_test.go`, `internal/orchestrator/reentry_test.go`.
- **Fake persona FS**: use `testing/fstest.MapFS` and point a
  `persona.FSLoader` at it. The workflow validator can take a
  stubbed loader (see `internal/workflow/validator_test.go`).
- **Fake repo**: `t.TempDir()` as the working directory; call
  `tools.RegisterBuiltins(reg, wd, run.Root)` so `write_file` paths
  resolve inside the temp dir.

## Workflow validation rules (what the validator enforces)

See `internal/workflow/validator.go`. If you add a field to the schema,
add the matching rule:

- required top-level fields (name, version, default_model, ≥1 stage)
- stage IDs unique; member IDs unique within their stage
- personas resolvable via the configured loader
- tools referenced by personas are registered
- artifact names globally unique
- `inputs` entries reference artifacts from *strictly prior* stages
- verdict routes target known stages; re-entry requires `max_reentries ≥ 1`
- task templates parse as Go `text/template`
- `{{ .vars.X }}` references must bind to a declared variable
- `on_fail` ∈ `{halt, retry, continue}`

## Release automation (Conventional Commits)

Every push to `main` is analyzed. The commit type at the **highest**
bump level across all commits since the last tag wins:

- `<type>!:` in subject or `BREAKING CHANGE:` in body → major
- `feat` → minor
- `fix`, `perf` → patch
- others → no release

Non-conventional commits contribute nothing. Write real conventional
messages or the release workflow skips them silently.

When an agent lands a user-visible change, the expected commit shape is
`feat(scope): …` or `fix(scope): …`. Refactors that don't change user
behavior are `refactor:` (no release cut) and should be paired with the
feat/fix commit that motivates them, or deferred.

## What to avoid

- Don't hardcode stage counts, stage names, or persona names anywhere
  in the orchestrator or agent layer.
- Don't add retries, fallbacks, or heuristics that "make the model
  behave better" inside the agent loop. The model's output is the
  model's output. If a persona produces bad output, edit the persona
  file, not the runtime.
- Don't add configuration for things that can be set in a persona
  file or a workflow file. The CLI's surface area is small on purpose.
- Don't reintroduce non-streaming chat. The event bus assumes streaming
  and the TUI renders deltas live.
- Don't `rm -rf` prior artifact directories. The re-entry archive is
  part of the audit trail.
- Don't write docs (new `.md` files) unless explicitly asked. This file
  is the exception.

## When you're not sure

- The design document is no longer in the repo (removed in favor of
  this file and the README). Reconstruct intent by reading the tests
  in `internal/orchestrator/` first — they exercise the contracts most
  densely.
- `internal/workflow/validator_test.go` shows the expected shape of
  valid and invalid workflows.
- The three shipped workflows in `internal/assets/workflows/` are the
  canonical examples of every schema feature.
