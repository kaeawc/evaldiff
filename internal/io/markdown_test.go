package io

import (
	"strings"
	"testing"

	"github.com/kaeawc/evaldiff/internal/coverage"
	"github.com/kaeawc/evaldiff/internal/diff"
	"github.com/kaeawc/evaldiff/internal/index"
	"github.com/kaeawc/evaldiff/internal/rank"
)

func TestRenderMarkdown_EmptyResult(t *testing.T) {
	got := RenderMarkdown(&rank.EvalsAtRisk{})
	if !strings.Contains(got, "no evals at risk") {
		t.Fatalf("expected 'no evals at risk' message, got %q", got)
	}
}

func TestRenderMarkdown_NilInput(t *testing.T) {
	got := RenderMarkdown(nil)
	if !strings.Contains(got, "no evals at risk") {
		t.Fatalf("expected 'no evals at risk' message, got %q", got)
	}
}

func TestRenderMarkdown_OneTestOneAddedTool(t *testing.T) {
	risk := &rank.EvalsAtRisk{
		Tests: []rank.EvalRisk{{
			Test:  coverage.TestEntry{File: "tests/test_x.py", Line: 1, Name: "test_one"},
			Score: 1.0,
			Affected: []rank.BehaviorChange{{
				Ref:   rank.BehaviorRef{Kind: "tool", File: "app/tools.py", Name: "browse"},
				Kind:  rank.ChangeAdded,
				Score: 1.0,
			}},
		}},
	}
	got := RenderMarkdown(risk)
	checks := []string{
		"### evaldiff: 1 eval at risk",
		"#### `tests/test_x.py::test_one` — score 1.00",
		"- **added** tool `browse` in `app/tools.py`",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in output:\n%s", want, got)
		}
	}
}

func TestRenderMarkdown_PluralizesEvalsCount(t *testing.T) {
	risk := &rank.EvalsAtRisk{
		Tests: []rank.EvalRisk{
			{Test: coverage.TestEntry{File: "a", Name: "t1"}, Affected: []rank.BehaviorChange{{Kind: rank.ChangeAdded}}},
			{Test: coverage.TestEntry{File: "b", Name: "t2"}, Affected: []rank.BehaviorChange{{Kind: rank.ChangeAdded}}},
		},
	}
	got := RenderMarkdown(risk)
	if !strings.Contains(got, "2 evals at risk") {
		t.Fatalf("expected plural 'evals', got %q", got)
	}
}

func TestRenderMarkdown_ClassMethodTestNameIncludesClass(t *testing.T) {
	risk := &rank.EvalsAtRisk{
		Tests: []rank.EvalRisk{{
			Test:     coverage.TestEntry{File: "tests/test_cls.py", Name: "test_method", Class: "TestX"},
			Affected: []rank.BehaviorChange{{Kind: rank.ChangeAdded, Ref: rank.BehaviorRef{Kind: "agent", File: "x.py", Name: "a"}}},
		}},
	}
	got := RenderMarkdown(risk)
	if !strings.Contains(got, "tests/test_cls.py::TestX.test_method") {
		t.Fatalf("expected class-qualified name, got %q", got)
	}
}

func TestRenderMarkdown_AgentSystemDiffInlineMarkdown(t *testing.T) {
	risk := &rank.EvalsAtRisk{
		Tests: []rank.EvalRisk{{
			Test: coverage.TestEntry{File: "tests/test_x.py", Name: "test_x"},
			Affected: []rank.BehaviorChange{{
				Ref:  rank.BehaviorRef{Kind: "agent", File: "app/a.py", Name: "researcher"},
				Kind: rank.ChangeModified,
				AgentMod: &diff.AgentMod{
					Fields: []string{"system"},
					SystemDiff: &diff.TextDiff{Segments: []diff.TextSegment{
						{Kind: diff.SegmentEqual, Text: "You are a "},
						{Kind: diff.SegmentRemoved, Text: "helpful "},
						{Kind: diff.SegmentAdded, Text: "very helpful "},
						{Kind: diff.SegmentEqual, Text: "assistant."},
					}},
				},
			}},
		}},
	}
	got := RenderMarkdown(risk)
	want := "> You are a ~~helpful ~~**very helpful **assistant."
	if !strings.Contains(got, want) {
		t.Fatalf("expected inline diff %q in:\n%s", want, got)
	}
	if !strings.Contains(got, "fields: `system`") {
		t.Fatalf("missing fields line, got:\n%s", got)
	}
}

func TestRenderMarkdown_ToolDescriptionDiffRendered(t *testing.T) {
	risk := &rank.EvalsAtRisk{
		Tests: []rank.EvalRisk{{
			Test: coverage.TestEntry{File: "tests/test_t.py", Name: "test_t"},
			Affected: []rank.BehaviorChange{{
				Ref:  rank.BehaviorRef{Kind: "tool", File: "app/t.py", Name: "search"},
				Kind: rank.ChangeModified,
				ToolMod: &diff.ToolMod{
					Fields: []string{"description"},
					DescriptionDiff: &diff.TextDiff{Segments: []diff.TextSegment{
						{Kind: diff.SegmentEqual, Text: "Search"},
						{Kind: diff.SegmentAdded, Text: " the web"},
						{Kind: diff.SegmentEqual, Text: "."},
					}},
				},
			}},
		}},
	}
	got := RenderMarkdown(risk)
	if !strings.Contains(got, "> Search** the web**.") {
		t.Fatalf("missing description diff render in:\n%s", got)
	}
}

func TestRenderMarkdown_ParamsDiffOneLineSummary(t *testing.T) {
	risk := &rank.EvalsAtRisk{
		Tests: []rank.EvalRisk{{
			Test: coverage.TestEntry{File: "tests/test_t.py", Name: "test_t"},
			Affected: []rank.BehaviorChange{{
				Ref:  rank.BehaviorRef{Kind: "tool", File: "app/t.py", Name: "search"},
				Kind: rank.ChangeModified,
				ToolMod: &diff.ToolMod{
					Fields: []string{"params"},
					ParamsDiff: &diff.ParamsDiff{
						Added:    []index.ToolParam{{Name: "lang"}},
						Removed:  []index.ToolParam{{Name: "limit"}},
						Modified: []diff.ParamMod{{Before: index.ToolParam{Name: "q"}, After: index.ToolParam{Name: "q"}, Fields: []string{"annotation"}}},
					},
				},
			}},
		}},
	}
	got := RenderMarkdown(risk)
	if !strings.Contains(got, "params: +`lang`, -`limit`, ~`q` (annotation)") {
		t.Fatalf("expected params line, got:\n%s", got)
	}
}

func TestRenderMarkdown_ParamsReorderOnly_ShowsReorderHint(t *testing.T) {
	risk := &rank.EvalsAtRisk{
		Tests: []rank.EvalRisk{{
			Test: coverage.TestEntry{File: "t.py", Name: "t"},
			Affected: []rank.BehaviorChange{{
				Ref:  rank.BehaviorRef{Kind: "tool", File: "x.py", Name: "f"},
				Kind: rank.ChangeModified,
				ToolMod: &diff.ToolMod{
					Fields:     []string{"params"},
					ParamsDiff: &diff.ParamsDiff{},
				},
			}},
		}},
	}
	got := RenderMarkdown(risk)
	if !strings.Contains(got, "_(reorder only)_") {
		t.Fatalf("expected reorder hint, got:\n%s", got)
	}
}

func TestRenderMarkdown_RemovedBehaviorsSection(t *testing.T) {
	risk := &rank.EvalsAtRisk{
		Removed: []rank.BehaviorRef{
			{Kind: "agent", File: "app/a.py", Name: "gone"},
			{Kind: "tool", File: "app/t.py", Name: "old_search"},
		},
	}
	got := RenderMarkdown(risk)
	checks := []string{
		"### Removed behaviors",
		"may fail to load",
		"agent `app/a.py::gone`",
		"tool `app/t.py::old_search`",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderMarkdown_MultilineSystemPromptKeepsBlockquote(t *testing.T) {
	risk := &rank.EvalsAtRisk{
		Tests: []rank.EvalRisk{{
			Test: coverage.TestEntry{File: "t.py", Name: "t"},
			Affected: []rank.BehaviorChange{{
				Ref:  rank.BehaviorRef{Kind: "agent", File: "x.py", Name: "a"},
				Kind: rank.ChangeModified,
				AgentMod: &diff.AgentMod{
					Fields: []string{"system"},
					SystemDiff: &diff.TextDiff{Segments: []diff.TextSegment{
						{Kind: diff.SegmentEqual, Text: "line one\nline two"},
					}},
				},
			}},
		}},
	}
	got := RenderMarkdown(risk)
	// Both lines must be prefixed with the same indent + "> ".
	if !strings.Contains(got, "    > line one\n    > line two\n") {
		t.Fatalf("multiline blockquote not formed correctly, got:\n%s", got)
	}
}
