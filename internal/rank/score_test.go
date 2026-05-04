package rank

import (
	"context"
	"math"
	"testing"

	"github.com/kaeawc/evaldiff/internal/coverage"
	"github.com/kaeawc/evaldiff/internal/diff"
	"github.com/kaeawc/evaldiff/internal/index"
	"github.com/kaeawc/evaldiff/internal/vfs"
)

func TestScoreChange_Added_Is_1(t *testing.T) {
	if got := scoreChange(BehaviorChange{Kind: ChangeAdded}); got != 1.0 {
		t.Fatalf("Added score = %v, want 1.0", got)
	}
}

func TestScoreChange_AgentModelOnly_Is_1(t *testing.T) {
	c := BehaviorChange{
		Kind:     ChangeModified,
		AgentMod: &diff.AgentMod{Fields: []string{"model"}},
	}
	if got := scoreChange(c); got != 1.0 {
		t.Fatalf("model-only score = %v, want 1.0", got)
	}
}

func TestScoreChange_AgentSystemEditRatio(t *testing.T) {
	// Construct a SystemDiff where 5 of 30 bytes changed.
	td := &diff.TextDiff{Segments: []diff.TextSegment{
		{Kind: diff.SegmentEqual, Text: "AAAAA"},                // 5
		{Kind: diff.SegmentAdded, Text: "BBBBB"},                // 5 (edit)
		{Kind: diff.SegmentEqual, Text: "CCCCCCCCCCCCCCCCCCCC"}, // 20
	}}
	c := BehaviorChange{
		Kind: ChangeModified,
		AgentMod: &diff.AgentMod{
			Fields:     []string{"system"},
			SystemDiff: td,
		},
	}
	got := scoreChange(c)
	want := 5.0 / 30.0
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("system-only edit ratio = %v, want %v", got, want)
	}
}

func TestScoreChange_AgentNilSystemDiff_Is_FullField(t *testing.T) {
	// Field flagged "system" but no TextDiff (e.g. literal → dynamic
	// transition). We can't see the magnitude, so count it as a full
	// field change.
	c := BehaviorChange{
		Kind:     ChangeModified,
		AgentMod: &diff.AgentMod{Fields: []string{"system"}},
	}
	if got := scoreChange(c); got != 1.0 {
		t.Fatalf("nil SystemDiff score = %v, want 1.0", got)
	}
}

func TestScoreChange_AgentMultipleFieldsAccumulate(t *testing.T) {
	td := &diff.TextDiff{Segments: []diff.TextSegment{
		{Kind: diff.SegmentEqual, Text: "AA"},
		{Kind: diff.SegmentRemoved, Text: "BB"},
	}}
	c := BehaviorChange{
		Kind: ChangeModified,
		AgentMod: &diff.AgentMod{
			Fields:     []string{"model", "system"},
			SystemDiff: td,
		},
	}
	want := 1.0 + 0.5 // model=1.0, system=2/4
	if got := scoreChange(c); math.Abs(got-want) > 1e-9 {
		t.Fatalf("multi-field score = %v, want %v", got, want)
	}
}

func TestScoreChange_ToolParamsCounted(t *testing.T) {
	c := BehaviorChange{
		Kind: ChangeModified,
		ToolMod: &diff.ToolMod{
			Fields: []string{"params"},
			ParamsDiff: &diff.ParamsDiff{
				Added:    []index.ToolParam{{Name: "lang"}},
				Removed:  []index.ToolParam{{Name: "limit"}},
				Modified: []diff.ParamMod{{Before: index.ToolParam{Name: "q"}, After: index.ToolParam{Name: "q"}}},
			},
		},
	}
	if got := scoreChange(c); got != 3.0 {
		t.Fatalf("params score = %v, want 3.0 (1+1+1)", got)
	}
}

func TestScoreChange_ToolMultipleFields(t *testing.T) {
	td := &diff.TextDiff{Segments: []diff.TextSegment{
		{Kind: diff.SegmentEqual, Text: "abcd"}, // 4 equal
		{Kind: diff.SegmentAdded, Text: "ef"},   // 2 edit
	}}
	c := BehaviorChange{
		Kind: ChangeModified,
		ToolMod: &diff.ToolMod{
			Fields:          []string{"description", "params"},
			DescriptionDiff: td,
			ParamsDiff: &diff.ParamsDiff{
				Added: []index.ToolParam{{Name: "x"}},
			},
		},
	}
	want := 2.0/6.0 + 1.0
	if got := scoreChange(c); math.Abs(got-want) > 1e-9 {
		t.Fatalf("tool multi-field score = %v, want %v", got, want)
	}
}

func TestCompute_TestsSortedByScoreDescending(t *testing.T) {
	// Two tests, one touching a small description tweak (low score),
	// the other touching a model change (high score). Ranking should
	// order them high → low.
	ctx := context.Background()
	base := vfs.NewMem(map[string]string{
		"/repo/app/agents.py": `from agents import Agent
big = Agent(name="big", model="sonnet", instructions="x")
`,
		"/repo/app/tools.py": `from agents import function_tool
@function_tool
def small(q: str): """Search."""
`,
	})
	head := vfs.NewMem(map[string]string{
		"/repo/app/agents.py": `from agents import Agent
big = Agent(name="big", model="opus",   instructions="x")
`,
		"/repo/app/tools.py": `from agents import function_tool
@function_tool
def small(q: str): """Searchz."""
`,
		"/repo/tests/test_big.py":   `from app.agents import big` + "\n" + `def test_big_change(): pass` + "\n",
		"/repo/tests/test_small.py": `from app.tools import small` + "\n" + `def test_small_change(): pass` + "\n",
	})
	baseIdx, _ := index.Build(ctx, base, "/repo")
	headIdx, _ := index.Build(ctx, head, "/repo")
	cov, _ := coverage.Build(ctx, head, "/repo")
	_ = coverage.AttachTouches(ctx, head, cov, headIdx)

	risk := Compute(diff.Diff(baseIdx, headIdx), headIdx, cov)
	if len(risk.Tests) != 2 {
		t.Fatalf("Tests count = %d, want 2", len(risk.Tests))
	}
	if risk.Tests[0].Test.Name != "test_big_change" {
		t.Fatalf("first should be test_big_change, got %s (score %v vs second %s score %v)",
			risk.Tests[0].Test.Name, risk.Tests[0].Score,
			risk.Tests[1].Test.Name, risk.Tests[1].Score)
	}
	if risk.Tests[0].Score <= risk.Tests[1].Score {
		t.Fatalf("expected first.Score > second.Score, got %v vs %v",
			risk.Tests[0].Score, risk.Tests[1].Score)
	}
}

func TestCompute_PerChangeScorePopulated(t *testing.T) {
	ctx := context.Background()
	base := vfs.NewMem(map[string]string{
		"/repo/app/agents.py": `from agents import Agent
a = Agent(name="a", model="m1", instructions="x")
`,
	})
	head := vfs.NewMem(map[string]string{
		"/repo/app/agents.py": `from agents import Agent
a = Agent(name="a", model="m2", instructions="x")
`,
		"/repo/tests/test_a.py": `from app.agents import a
def test_a(): pass
`,
	})
	baseIdx, _ := index.Build(ctx, base, "/repo")
	headIdx, _ := index.Build(ctx, head, "/repo")
	cov, _ := coverage.Build(ctx, head, "/repo")
	_ = coverage.AttachTouches(ctx, head, cov, headIdx)

	risk := Compute(diff.Diff(baseIdx, headIdx), headIdx, cov)
	if len(risk.Tests) != 1 {
		t.Fatalf("Tests count = %d, want 1", len(risk.Tests))
	}
	if got := risk.Tests[0].Affected[0].Score; got != 1.0 {
		t.Fatalf("BehaviorChange.Score = %v, want 1.0 (model field)", got)
	}
	if risk.Tests[0].Score != 1.0 {
		t.Fatalf("EvalRisk.Score = %v, want 1.0", risk.Tests[0].Score)
	}
}
