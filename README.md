# evaldiff — a static "which evals will this PR break?" analyzer

## What you're building

Inside any company shipping LLM products, the bottleneck on velocity is eval cost. Running the full eval suite on every PR is too slow and too expensive; running a curated subset misses regressions. Reviewers eyeball prompt diffs and guess which behaviors might have changed. This tool replaces the guess with a static answer: given a PR, output the ranked list of evals most likely to be affected, with a reason for each.

Borrow Krit's shape: Go-first, tree-sitter, capability-gated indexes, single-pass dispatch, multi-frontend output. Read `~/kaeawc/krit/CLAUDE.md` first — you are extending that pattern to a new domain (prompt/tool/eval graphs instead of source-code rules).

## Core idea

Two artifacts power everything:

1. **Behavior index** — for every commit, build a hash-addressable index of: prompt templates (normalized), tool schemas, agent edges, model IDs, sampling params, system messages, tokenizer choices.
2. **Eval-coverage index** — for every eval, statically infer which behaviors it exercises (by following its setup code, the agents it instantiates, the prompts it loads).

Then "which evals does this PR affect" reduces to: diff the behavior index between base and head, intersect with the eval-coverage index, rank by edit distance and proximity in the agent graph.

## Architecture

- **`cmd/evaldiff/`** — CLI: `evaldiff <base> <head>` prints affected evals.
- **`internal/index/`** — behavior + eval-coverage indexes, content-addressable, cached per commit.
- **`internal/diff/`** — semantic diff over the behavior index (not text diff — token-level diff over normalized prompt templates, structural diff over JSON schemas, edge diff over agent graphs).
- **`internal/rank/`** — heuristics: textual edit distance, schema field add/remove weight, agent-graph reachability from changed node.
- **`internal/io/`** — output formats: PR comment markdown, JSON, SARIF (with each affected eval as a "finding").
- **MCP server** — let an agent ask "what evals are at risk for this branch" during dev.

## Capabilities (Krit-style, opt-in)

- `NeedsBehaviorIndex` — most rules.
- `NeedsEvalCoverage` — only the ranking phase.
- `NeedsRuntime` — optional: actually load the Python eval module under a sandbox to resolve dynamic prompt loading. Off by default; falls back to AST-only.

## MVP

1. Behavior indexer for one framework (start with Claude Agent SDK or OpenAI Agents SDK).
2. Coverage indexer that walks pytest-style eval files and maps each test to the agents/prompts it touches.
3. Diff + rank + markdown PR-comment output.
4. GitHub Action that posts the comment.
5. Validate on a corpus of real PRs from open-source agent projects: hand-label which evals should have been re-run, compare to the tool's output.

## Stretch

- **Confidence intervals** — "this eval is 0.93 likely affected" based on historical correlation between behavior-index hash deltas and observed eval-score deltas. Requires telemetry feed.
- **Recommended eval subset** — given a budget of N tokens, pick the N evals with highest expected information gain.
- **Inverse mode** — given a failing eval, produce the minimal commit slice that would reproduce it (synergy with the repro-bundle extractor in idea #7).
- **VS Code lens** — show "12 evals at risk" inline on a prompt edit.

## Why this is interesting

Eval cost is the dominant friction in LLM product development, and nobody has good static tools for it. Doing this well requires the same primitives Krit already has (cross-file index, capability gates, fast incremental cache) plus a new modality (semantic diff over prompts/tools/graphs). The output is immediately legible to any engineer reading a PR.

## Non-goals

- Running the evals (a separate concern).
- Quality scoring of prompts.
- Anything that requires production telemetry — works on the source tree alone.
