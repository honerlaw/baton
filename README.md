# baton

A terminal tool that runs a quality-focused multi-stage agentic workflow
against your codebase. The default pipeline is seven stages — context →
design → parallel design review → design synthesis → implementation →
parallel code review → final verdict — with enforced written artifacts,
role-shaped personas, and fresh-context review.

The runtime is workflow-agnostic: workflows are YAML files, not code.
Edit the default workflow, write a new one, or pick the shorter built-in
one for smaller changes.

## Requirements

- Go 1.22+ (tested on 1.24)
- An [OpenRouter](https://openrouter.ai) API key

## Install

### One-liner (Linux/macOS)

```sh
curl -sSfL https://raw.githubusercontent.com/honerlaw/baton/main/install.sh | bash
```

The script detects your OS/arch, downloads the matching release tarball,
verifies its SHA256 against the release's `checksums.txt`, and installs
the binary to `/usr/local/bin` (or `$HOME/.local/bin` if that isn't
writable).

Pin a version or change the install directory:

```sh
VERSION=v1.0.0 curl -sSfL https://raw.githubusercontent.com/honerlaw/baton/main/install.sh | bash
INSTALL_DIR=$HOME/bin curl -sSfL https://raw.githubusercontent.com/honerlaw/baton/main/install.sh | bash
```

### From source

```sh
go build -o baton ./cmd/baton
# or:
go install github.com/honerlaw/baton/cmd/baton@latest
```

### Manual download

Prebuilt binaries are attached to each [GitHub
release](https://github.com/honerlaw/baton/releases) for
linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, and windows/amd64.
Unpack the archive and move `baton` onto your `$PATH`.

## Quick start

```sh
export OPENROUTER_API_KEY=sk-or-...
cd your-project

# Write the default personas and workflows into the project
# (.claude/agents/*.md and .baton/workflows/*.yaml)
baton init

# Run the default seven-stage workflow against a feature request
baton run --feature "add a --json flag to the export command"
```

Artifacts land in `.baton/runs/<ulid>/`:

```
.baton/runs/01HXYZ.../
  workflow.yaml          # copy of the workflow that ran
  variables.json         # resolved variables
  events.ndjson          # append-only event log
  artifacts/
    context.md
    design.md
    review-design-*.md   # three parallel reviews
    design-final.md
    design-response.md
    implementation-generalist.md
    review-code-*.md     # three parallel reviews
    verdict.md           # final decision, machine-parsable
  stages/<id>/...        # per-stage transcripts and usage
```

## Commands

| Command | Description |
|---|---|
| `baton run` | Execute a workflow. Auto-detects TTY; uses Bubble Tea TUI when interactive, plaintext otherwise. |
| `baton validate <file>` | Validate a workflow YAML file. Exits 2 on errors. |
| `baton init` | Scaffold embedded personas and workflows into `.claude/agents/` and `.baton/workflows/`. |
| `baton personas` | List known personas (project overrides + embedded defaults). |
| `baton version` | Print the binary version. |

Common `run` flags:

```
-w, --workflow    embedded workflow name: default | minimal | iter-design
-f, --file        workflow YAML path (overrides --workflow)
    --feature     shortcut for --var feature_request=...
    --var         extra variable, name=value (repeatable)
    --model       override the default model
    --no-tui      force plaintext renderer
    --artifacts-dir   where to write runs (default .baton/runs)
```

## The default workflow

Seven stages, shipped as `.baton/workflows/default.yaml`:

1. **context** — `codebase-scout` produces `context.md` (inventory only, file-path citations).
2. **design** — `design-author` produces `design.md` (one approach, concrete changes, failure modes).
3. **design-review** — three reviewers in parallel (`correctness`, `risk`, `simplicity`), each with fresh context.
4. **design-synthesis** — `design-synthesizer` produces `design-final.md` and `design-response.md`.
5. **implementation** — `impl-generalist` writes code and `implementation-generalist.md`.
6. **code-review** — three reviewers in parallel (`conformance`, `correctness`, `tests`).
7. **verdict** — `final-synthesizer` produces `verdict.md` with a fenced JSON block. Routes: `accept`, `revise_impl` (→ stage 5), or `revise_design` (→ stage 2).

Re-entry is capped by `max_reentries` (workflow default 1, overridable per stage). When a stage is re-entered, artifacts produced at or after that stage are moved to `artifacts.prev-N/` and remain available to templates via `{{ .prev.<name> }}`.

## Alternative shipped workflows

- **minimal** (3 stages) — context, implement, review. Small changes.
- **iter-design** — design iterates up to 3 revision rounds, then implement.

```sh
baton run --workflow minimal --feature "..."
baton run --workflow iter-design --feature "..."
```

## Writing a custom workflow

Copy one of the shipped files as a starting point:

```sh
cp .baton/workflows/default.yaml .baton/workflows/my-flow.yaml
```

The schema is concise. Each stage names a persona, a task template, the
artifacts it reads, and the artifact name it writes. Parallel groups use
`parallel: true` + a `members:` list. Verdicts route to a prior stage ID
or `""` for workflow completion.

```yaml
name: my-flow
version: 1.0.0
default_model: anthropic/claude-sonnet-4
max_reentries: 1

variables:
  - name: feature_request
    required: true

stages:
  - id: context
    persona: codebase-scout
    artifact: context.md
    task: |
      Feature: {{ .vars.feature_request }}
      Produce an inventory of relevant code.

  - id: design
    persona: design-author
    inputs: [context.md]
    artifact: design.md
    task: "Design the feature."

  - id: review
    parallel: true
    members:
      - id: correctness
        persona: design-reviewer-correctness
        inputs: [context.md, design.md]
        artifact: review-correctness.md
        task: "Review for correctness."
      - id: risk
        persona: design-reviewer-risk
        inputs: [context.md, design.md]
        artifact: review-risk.md
        task: "Review for risk."
```

Validate before running:

```sh
baton validate .baton/workflows/my-flow.yaml
baton run --file .baton/workflows/my-flow.yaml --feature "..."
```

Validation is strict: unknown personas, unknown artifact references, missing
required fields, undeclared variable references, and verdict routes without
enough re-entry budget all produce errors with file:line locations.

## Writing a custom persona

Personas live in `.claude/agents/*.md` and use the Claude-format
frontmatter. Project files take precedence over embedded defaults, so
you can override a shipped persona by writing a file with the same name.

```markdown
---
name: my-reviewer
description: What this persona does in one line.
model: anthropic/claude-sonnet-4      # optional override
tools: read_file, search              # optional allowlist
---

Body is the system prompt. Follow behavior-first style:

## How you think
- ...

## Checklist before you finish
- [ ] ...

## Escalation
Write `HALT: <reason>` to your artifact if ...

## Scope
Exactly what this persona does, and what it does not.
```

Principles the shipped personas follow: behavior-first (not credentials),
explicit checklists (not vague guidance), concrete escalation rules,
scoped responsibilities (a reviewer reviews, it does not implement even
if it notices something). Persona quality is load-bearing — edit the
shipped ones if they don't fit your codebase.

## Tools personas can call

| Tool | Purpose |
|---|---|
| `read_file` | Read a file in the working dir or run dir. |
| `write_file` | Write a file. Path must be inside the working dir or run dir. |
| `list_files` | Doublestar glob (`**/*.go`). |
| `search` | Content search via `rg` if present, else a pure-Go walk. |
| `bash` | Execute a shell command. Default timeout 60s, max 600s. |

Personas declare which tools they may call via the `tools:` frontmatter
field. An empty or missing list means all tools are permitted.

## Configuration

Precedence (high → low):

- `BATON_MODEL` env var (overrides `default_model`)
- `BATON_ARTIFACTS_ROOT` env var
- `$XDG_CONFIG_HOME/baton/config.yaml` (falls back to `~/.config/baton/config.yaml`)
- Compiled-in defaults

Example `config.yaml`:

```yaml
default_model: anthropic/claude-sonnet-4
api_key_env_var: OPENROUTER_API_KEY
artifacts_root: .baton/runs
```

Model resolution precedence within a run:
persona frontmatter > stage `model:` > workflow `default_model:` > tool-level default.

## Key bindings (TUI)

```
q, Ctrl-C    quit
g, G         scroll to top / bottom
↑↓ PgUp PgDn scroll
```

## Architecture

Headless core emits typed events to a fan-out bus. Subscribers (TUI,
plaintext renderer, NDJSON file logger) run concurrently. The orchestrator
has no knowledge of stage semantics — workflows are data.

```
cmd/baton/              cobra entrypoint
internal/orchestrator/  workflow engine (sequential, parallel, re-entry)
internal/workflow/      YAML schema, loader, validator, template resolver
internal/persona/       Claude-format parser + chained loader
internal/agent/         single-stage streaming agent loop
internal/openrouter/    streaming SSE client
internal/tools/         Tool interface, registry, built-ins
internal/event/         event types + fan-out bus
internal/artifact/      run-directory layout, re-entry archive
internal/tui/           Bubble Tea model
internal/render/        plaintext + NDJSON renderers
internal/config/        XDG config + env vars
internal/assets/        embedded personas and workflows + scaffolder
```

## Testing and linting

```sh
make test     # go test -race -count=1 ./...
make lint     # golangci-lint run (install once with: make tools)
make ci       # vet + test + lint (what CI runs)
make fmt      # gofumpt + goimports
```

Integration tests cover the full orchestrator end-to-end with a mock
OpenRouter, including parallel fan-out and verdict re-entry.
`.github/workflows/ci.yml` gates every PR on `go vet`,
`go test -race`, and `golangci-lint` with the repo's `.golangci.yml`.

## Releasing

Releases are automated from [Conventional
Commits](https://www.conventionalcommits.org/). On every push to `main`,
`.github/workflows/auto-release.yml` inspects commits since the last
`v*.*.*` tag and picks a bump:

| Commit pattern                           | Bump   |
|------------------------------------------|--------|
| `<type>!: …` in subject, or `BREAKING CHANGE: …` in body | major |
| `feat: …` / `feat(scope): …`             | minor  |
| `fix: …`, `perf: …`                      | patch  |
| `chore`, `docs`, `test`, `refactor`, `ci`, `build`, `style`, or no conventional prefix | no release |

The workflow picks the highest bump across all commits since the last
tag. When there's something to ship, it creates an annotated tag
(e.g. `v0.3.0`) and invokes the release workflow, which builds the
matrix and publishes binaries + `checksums.txt` to a GitHub release.

Manual overrides:

- **Force a bump:** run `auto-release` via `workflow_dispatch` and pick
  `patch` / `minor` / `major`.
- **Release an arbitrary tag:** run `release` via `workflow_dispatch`
  with the `tag` input, or push a tag manually
  (`git tag v1.2.3 && git push origin v1.2.3`).
