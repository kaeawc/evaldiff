package diff

import (
	"context"
	"reflect"
	"testing"

	"github.com/kaeawc/evaldiff/internal/index"
	"github.com/kaeawc/evaldiff/internal/vfs"
)

func TestDiff_NoChanges(t *testing.T) {
	src := map[string]string{
		"/repo/a.py": `Agent(model="m", system="s")
@tool
def t():
    """d."""
`,
	}
	cs := Diff(buildIdx(t, src, "/repo"), buildIdx(t, src, "/repo"))
	if !cs.IsEmpty() {
		t.Fatalf("changeset should be empty, got %+v", cs)
	}
}

func TestDiff_AddedAgent(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="m1")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="m1")
Agent(model="m2")`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Agents.Added) != 1 || cs.Agents.Added[0].Model.Str != "m2" {
		t.Fatalf("Added: %+v", cs.Agents.Added)
	}
	if len(cs.Agents.Removed) != 0 || len(cs.Agents.Modified) != 0 {
		t.Fatalf("only Added expected, got %+v", cs.Agents)
	}
}

func TestDiff_RemovedAgent(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="m1")
Agent(model="m2")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="m1")`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Agents.Removed) != 1 || cs.Agents.Removed[0].Model.Str != "m2" {
		t.Fatalf("Removed: %+v", cs.Agents.Removed)
	}
}

func TestDiff_ModifiedAgent_ReportsChangedFields(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="sonnet", system="be helpful")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="opus", system="be helpful")`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Agents.Modified) != 1 {
		t.Fatalf("Modified count = %d, want 1; got %+v", len(cs.Agents.Modified), cs.Agents)
	}
	mod := cs.Agents.Modified[0]
	if !reflect.DeepEqual(mod.Fields, []string{"model"}) {
		t.Fatalf("Fields = %v, want [model]", mod.Fields)
	}
	if mod.Before.Model.Str != "sonnet" || mod.After.Model.Str != "opus" {
		t.Fatalf("Before/After model: %+v / %+v", mod.Before.Model, mod.After.Model)
	}
}

func TestDiff_AgentMoveWithinFile_NotADifference(t *testing.T) {
	// Moving an Agent down by adding a comment above it must not trigger a
	// modification — File and Line are excluded from the field diff. This
	// pins the "ordinal-within-file" identity heuristic.
	base := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="m")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/a.py": `# new comment

Agent(model="m")`,
	}, "/repo")
	cs := Diff(base, head)
	if !cs.IsEmpty() {
		t.Fatalf("expected no changes after a vertical shift, got %+v", cs)
	}
}

func TestDiff_AgentMovedAcrossFiles_AppearsAsRemoveAndAdd(t *testing.T) {
	// File-keyed identity does not match cross-file moves. Documenting that
	// limitation in a test so it's explicit, not surprising.
	base := buildIdx(t, map[string]string{
		"/repo/old.py": `Agent(model="m")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/new.py": `Agent(model="m")`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Agents.Added) != 1 || len(cs.Agents.Removed) != 1 {
		t.Fatalf("expected 1 added + 1 removed, got %+v", cs.Agents)
	}
	if len(cs.Agents.Modified) != 0 {
		t.Fatalf("did not expect Modified, got %+v", cs.Agents.Modified)
	}
}

func TestDiff_ToolAddRemoveModify(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/t.py": `@tool
def removed(q: str):
    """gone next."""

@tool
def stays(q: str):
    """v1."""
`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/t.py": `@tool
def stays(q: str):
    """v2."""

@tool
def fresh():
    """new."""
`,
	}, "/repo")
	cs := Diff(base, head)
	if got := names(cs.Tools.Added); !reflect.DeepEqual(got, []string{"fresh"}) {
		t.Fatalf("Added = %v, want [fresh]", got)
	}
	if got := names(cs.Tools.Removed); !reflect.DeepEqual(got, []string{"removed"}) {
		t.Fatalf("Removed = %v, want [removed]", got)
	}
	if len(cs.Tools.Modified) != 1 {
		t.Fatalf("Modified count = %d, want 1; got %+v", len(cs.Tools.Modified), cs.Tools)
	}
	mod := cs.Tools.Modified[0]
	if mod.Before.Name != "stays" || !reflect.DeepEqual(mod.Fields, []string{"description"}) {
		t.Fatalf("modified entry: %+v", mod)
	}
}

func TestDiff_ToolParamChange_ReportedUnderParams(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/t.py": `@tool
def f(a: str):
    """d."""
`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/t.py": `@tool
def f(a: str, b: int = 0):
    """d."""
`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Tools.Modified) != 1 {
		t.Fatalf("Modified count = %d", len(cs.Tools.Modified))
	}
	if !reflect.DeepEqual(cs.Tools.Modified[0].Fields, []string{"params"}) {
		t.Fatalf("Fields = %v, want [params]", cs.Tools.Modified[0].Fields)
	}
}

func TestDiff_NilIndexes_BehaveAsEmpty(t *testing.T) {
	cs := Diff(nil, nil)
	if !cs.IsEmpty() {
		t.Fatalf("expected empty changeset, got %+v", cs)
	}
	head := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="m")`,
	}, "/repo")
	cs = Diff(nil, head)
	if len(cs.Agents.Added) != 1 {
		t.Fatalf("expected 1 added when base is nil, got %+v", cs.Agents)
	}
}

func buildIdx(t *testing.T, files map[string]string, root string) *index.Index {
	t.Helper()
	idx, err := index.Build(context.Background(), vfs.NewMem(files), root)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return idx
}

func names(tools []index.Tool) []string {
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i] = t.Name
	}
	return out
}
