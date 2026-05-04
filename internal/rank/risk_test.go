package rank

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/kaeawc/evaldiff/internal/coverage"
	"github.com/kaeawc/evaldiff/internal/diff"
	"github.com/kaeawc/evaldiff/internal/index"
	"github.com/kaeawc/evaldiff/internal/vfs"
)

// twoTreeFixture builds head/base/coverage from two parallel filesystems
// so each test case can describe one self-contained scenario.
type twoTreeFixture struct {
	base, head map[string]string
	tests      map[string]string // additional files merged into the head tree
}

func (f twoTreeFixture) build(t *testing.T) (baseIdx, headIdx *index.Index, cov *coverage.Coverage) {
	t.Helper()
	baseFs := vfs.NewMem(f.base)
	headFs := vfs.NewMem(merge(f.head, f.tests))
	ctx := context.Background()
	var err error
	baseIdx, err = index.Build(ctx, baseFs, "/repo")
	if err != nil {
		t.Fatalf("base index.Build: %v", err)
	}
	headIdx, err = index.Build(ctx, headFs, "/repo")
	if err != nil {
		t.Fatalf("head index.Build: %v", err)
	}
	cov, err = coverage.Build(ctx, headFs, "/repo")
	if err != nil {
		t.Fatalf("coverage.Build: %v", err)
	}
	if err := coverage.AttachTouches(ctx, headFs, cov, headIdx); err != nil {
		t.Fatalf("AttachTouches: %v", err)
	}
	return
}

func TestCompute_TestTouchingModifiedAgent_IsAtRisk(t *testing.T) {
	f := twoTreeFixture{
		base: map[string]string{
			"/repo/app/agents.py": `from agents import Agent
researcher = Agent(name="researcher", model="sonnet", instructions="research")
`,
		},
		head: map[string]string{
			"/repo/app/agents.py": `from agents import Agent
researcher = Agent(name="researcher", model="opus", instructions="research")
`,
		},
		tests: map[string]string{
			"/repo/tests/test_pipeline.py": `from app.agents import researcher
def test_uses_researcher():
    researcher.run("hi")
`,
		},
	}
	base, head, cov := f.build(t)
	cs := diff.Diff(base, head)

	risk := Compute(cs, head, cov)
	if len(risk.Tests) != 1 {
		t.Fatalf("Tests count = %d, want 1; got %+v", len(risk.Tests), risk)
	}
	r := risk.Tests[0]
	if r.Test.Name != "test_uses_researcher" {
		t.Fatalf("test name = %q", r.Test.Name)
	}
	if len(r.Affected) != 1 {
		t.Fatalf("Affected count = %d", len(r.Affected))
	}
	if r.Affected[0].Kind != ChangeModified ||
		r.Affected[0].Ref.Name != "researcher" ||
		r.Affected[0].AgentMod == nil {
		t.Fatalf("affected entry: %+v", r.Affected[0])
	}
	if got := r.Affected[0].AgentMod.Fields; !reflect.DeepEqual(got, []string{"model"}) {
		t.Fatalf("AgentMod.Fields = %v, want [model]", got)
	}
}

func TestCompute_TestTouchingAddedToolIsAtRisk(t *testing.T) {
	f := twoTreeFixture{
		base: map[string]string{
			"/repo/app/tools.py": `from agents import function_tool
@function_tool
def search(q: str): """Search."""
`,
		},
		head: map[string]string{
			"/repo/app/tools.py": `from agents import function_tool
@function_tool
def search(q: str): """Search."""
@function_tool
def browse(url: str): """Browse."""
`,
		},
		tests: map[string]string{
			"/repo/tests/test_x.py": `from app.tools import search, browse
def test_browse():
    browse("u"); search("q")
`,
		},
	}
	base, head, cov := f.build(t)
	cs := diff.Diff(base, head)

	risk := Compute(cs, head, cov)
	if len(risk.Tests) != 1 {
		t.Fatalf("Tests count = %d, want 1; got %+v", len(risk.Tests), risk)
	}
	if len(risk.Tests[0].Affected) != 1 ||
		risk.Tests[0].Affected[0].Kind != ChangeAdded ||
		risk.Tests[0].Affected[0].Ref.Name != "browse" {
		t.Fatalf("expected one ChangeAdded for `browse`, got %+v", risk.Tests[0].Affected)
	}
}

func TestCompute_OnlyReferencingTestsAreAtRisk(t *testing.T) {
	// Under per-test refinement, a test is at risk only if a changed
	// behavior's imported name appears in its body. test_b references b
	// (which changed model), test_a_via_b imports both a and b but only
	// uses b in the body — and a is what changed.
	f := twoTreeFixture{
		base: map[string]string{
			"/repo/app/agents.py": `from agents import Agent
a = Agent(name="a", model="m", instructions="x")
b = Agent(name="b", model="m", instructions="y")
`,
		},
		head: map[string]string{
			"/repo/app/agents.py": `from agents import Agent
a = Agent(name="a", model="m2", instructions="x")
b = Agent(name="b", model="m2", instructions="y")
`,
		},
		tests: map[string]string{
			// References b (which changed model) — at risk.
			"/repo/tests/test_b.py": `from app.agents import b
def test_b():
    b.run("hi")
`,
			// Imports both but only references a — touches a (which also
			// changed) — at risk.
			"/repo/tests/test_a.py": `from app.agents import a, b
def test_a_only():
    a.run("hi")
`,
			// Imports nothing related — not at risk.
			"/repo/tests/test_unrelated.py": `def test_x():
    print("nothing imported")
`,
		},
	}
	base, head, cov := f.build(t)
	cs := diff.Diff(base, head)
	risk := Compute(cs, head, cov)

	names := []string{}
	for _, r := range risk.Tests {
		names = append(names, r.Test.Name)
	}
	sort.Strings(names)
	want := []string{"test_a_only", "test_b"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("at-risk test names = %v, want %v", names, want)
	}
}

func TestCompute_RemovedBehaviorsListed(t *testing.T) {
	f := twoTreeFixture{
		base: map[string]string{
			"/repo/app/agents.py": `from agents import Agent
researcher = Agent(name="researcher", model="m", instructions="x")
gone       = Agent(name="gone",       model="m", instructions="y")
`,
		},
		head: map[string]string{
			"/repo/app/agents.py": `from agents import Agent
researcher = Agent(name="researcher", model="m", instructions="x")
`,
		},
		tests: map[string]string{},
	}
	base, head, cov := f.build(t)
	cs := diff.Diff(base, head)
	risk := Compute(cs, head, cov)

	if len(risk.Removed) != 1 ||
		risk.Removed[0].Kind != "agent" ||
		risk.Removed[0].Name != "gone" {
		t.Fatalf("Removed = %+v, want [{agent, app/agents.py, gone}]", risk.Removed)
	}
}

func TestCompute_NoChangesYieldsEmptyResult(t *testing.T) {
	src := map[string]string{
		"/repo/app/agents.py": `from agents import Agent
a = Agent(name="a", model="m", instructions="x")
`,
		"/repo/tests/test_x.py": `from app.agents import a
def test_a():
    a.run("hi")
`,
	}
	f := twoTreeFixture{base: src, head: src}
	base, head, cov := f.build(t)
	cs := diff.Diff(base, head)
	risk := Compute(cs, head, cov)
	if len(risk.Tests) != 0 || len(risk.Removed) != 0 {
		t.Fatalf("expected empty risk, got %+v", risk)
	}
}

func TestCompute_NilInputsAreNoOps(t *testing.T) {
	if got := Compute(nil, nil, nil); got == nil {
		t.Fatal("Compute(nil, nil, nil) should return non-nil empty result")
	}
}

func TestCompute_TwoChangesOnSameTestCollapseUnderOneTestEntry(t *testing.T) {
	f := twoTreeFixture{
		base: map[string]string{
			"/repo/app/agents.py": `from agents import Agent
a = Agent(name="a", model="m1", instructions="x")
`,
			"/repo/app/tools.py": `from agents import function_tool
@function_tool
def search(q: str): """Search."""
`,
		},
		head: map[string]string{
			"/repo/app/agents.py": `from agents import Agent
a = Agent(name="a", model="m2", instructions="x")
`,
			"/repo/app/tools.py": `from agents import function_tool
@function_tool
def search(q: str): """Search the web."""
`,
		},
		tests: map[string]string{
			"/repo/tests/test_pipeline.py": `from app.agents import a
from app.tools  import search
def test_pipeline():
    a.run("hi"); search("q")
`,
		},
	}
	base, head, cov := f.build(t)
	cs := diff.Diff(base, head)
	risk := Compute(cs, head, cov)
	if len(risk.Tests) != 1 {
		t.Fatalf("Tests count = %d, want 1 (one test, two affected behaviors)", len(risk.Tests))
	}
	if len(risk.Tests[0].Affected) != 2 {
		t.Fatalf("Affected count = %d, want 2; got %+v", len(risk.Tests[0].Affected), risk.Tests[0].Affected)
	}
}

func merge(a, b map[string]string) map[string]string {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
