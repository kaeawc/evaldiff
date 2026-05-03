# evaldiff

Static "which evals will this PR break?" analyzer. Given a base and head
commit, evaldiff diffs the behavior index (prompt templates, tool schemas,
agent edges, model IDs, sampling params, system messages, tokenizer choices),
intersects with the eval-coverage index, and prints the ranked list of
affected evals with a reason for each.

Borrows Krit's shape: Go-first, tree-sitter, capability-gated indexes,
single-pass dispatch, multi-frontend output. Read `~/kaeawc/krit/CLAUDE.md`
for the originating pattern. evaldiff applies it to a different domain
(prompt/tool/eval graphs in Python eval suites instead of Kotlin/Java source
rules).

## Key rules

- Keep analyzer work in Go. A small Python sidecar may exist later for the
  optional `NeedsRuntime` capability (loading a Python eval module under a
  sandbox to resolve dynamic prompt loading); the Go core handles the
  static-AST cases that cover most evals.
- After implementation changes, run `go build ./cmd/evaldiff/ && go vet ./...`.
- Run `go test ./... -count=1` for full validation.
- Use tree-sitter AST for structural extraction; reserve regex for
  line-oriented checks.
- Indexes are content-addressable and cached per commit.

## Project structure

- `cmd/evaldiff/` — CLI entry point. `evaldiff <base> <head>` prints affected evals.
- `internal/index/` — behavior + eval-coverage indexes.
- `internal/diff/` — semantic diff over the behavior index (token diff over
  normalized prompts, structural diff over JSON schemas, edge diff over
  agent graphs).
- `internal/rank/` — ranking heuristics (edit distance, schema field weights,
  agent-graph reachability).
- `internal/io/` — output formats (PR comment markdown, JSON, SARIF).

## Capabilities (Krit-style, opt-in)

- `NeedsBehaviorIndex` — most rules.
- `NeedsEvalCoverage` — only the ranking phase.
- `NeedsRuntime` — optional Python sidecar for dynamic prompt resolution.
  Off by default; falls back to AST-only.

## Build & validate

```bash
make build       # go build -o evaldiff ./cmd/evaldiff/
make test        # go test ./... -count=1
make vet         # go vet ./...
make lint        # golangci-lint run
make ci          # vet + test + complexity + lint + security + licenses
```

## Status

Bootstrap only. Behavior indexer, diff, rank, and output are stubs.
First vertical slice: behavior indexer for the Claude Agent SDK (Python),
exposed via `evaldiff index <dir>`.
