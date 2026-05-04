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

func TestDiff_UnnamedAgentMovedAcrossFiles_AppearsAsRemoveAndAdd(t *testing.T) {
	// Cross-file move reconciliation requires a literal name kwarg —
	// unnamed agents have no stable identity to match across files,
	// so they correctly surface as remove + add.
	base := buildIdx(t, map[string]string{
		"/repo/old.py": `Agent(model="m")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/new.py": `Agent(model="m")`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Agents.Added) != 1 || len(cs.Agents.Removed) != 1 {
		t.Fatalf("expected 1 added + 1 removed for unnamed move, got %+v", cs.Agents)
	}
	if len(cs.Agents.Modified) != 0 {
		t.Fatalf("did not expect Modified, got %+v", cs.Agents.Modified)
	}
}

func TestDiff_NamedAgentMovedAcrossFiles_BecomesModifiedWithFileField(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/old.py": `Agent(name="researcher", model="m", instructions="research")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/new.py": `Agent(name="researcher", model="m", instructions="research")`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Agents.Added) != 0 || len(cs.Agents.Removed) != 0 {
		t.Fatalf("named cross-file move should not surface as add/remove, got %+v", cs.Agents)
	}
	if len(cs.Agents.Modified) != 1 {
		t.Fatalf("expected 1 Modified, got %+v", cs.Agents)
	}
	mod := cs.Agents.Modified[0]
	if !reflect.DeepEqual(mod.Fields, []string{"file"}) {
		t.Fatalf("Fields = %v, want [file] (no other kwargs changed)", mod.Fields)
	}
	if mod.Before.File != "old.py" || mod.After.File != "new.py" {
		t.Fatalf("Before/After files: %q / %q", mod.Before.File, mod.After.File)
	}
}

func TestDiff_NamedAgentMovedAndEdited_FieldsIncludesFileAndModel(t *testing.T) {
	// Move + model edit in the same change. Reconcile should emit
	// one Modified entry with both file and model in Fields.
	base := buildIdx(t, map[string]string{
		"/repo/old.py": `Agent(name="researcher", model="sonnet", instructions="research")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/new.py": `Agent(name="researcher", model="opus",   instructions="research")`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Agents.Modified) != 1 {
		t.Fatalf("expected 1 Modified, got %+v", cs.Agents)
	}
	got := cs.Agents.Modified[0].Fields
	want := []string{"file", "model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Fields = %v, want %v", got, want)
	}
}

func TestDiff_AmbiguousNamedAgentMove_StaysAsRemoveAndAdd(t *testing.T) {
	// Two agents named "x" on the head side — we can't tell which
	// removed agent maps to which, so neither is reconciled.
	base := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(name="x", model="m")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/b.py": `Agent(name="x", model="m")`,
		"/repo/c.py": `Agent(name="x", model="m")`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Agents.Added) != 2 || len(cs.Agents.Removed) != 1 || len(cs.Agents.Modified) != 0 {
		t.Fatalf("ambiguous case should stay unreconciled, got %+v", cs.Agents)
	}
}

func TestDiff_ToolMovedAcrossFiles_BecomesModifiedWithFileField(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/old.py": `@tool
def search(q: str): """Search."""
`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/new.py": `@tool
def search(q: str): """Search."""
`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Tools.Added) != 0 || len(cs.Tools.Removed) != 0 {
		t.Fatalf("tool move should not surface as add/remove, got %+v", cs.Tools)
	}
	if len(cs.Tools.Modified) != 1 {
		t.Fatalf("expected 1 Modified, got %+v", cs.Tools)
	}
	mod := cs.Tools.Modified[0]
	if !reflect.DeepEqual(mod.Fields, []string{"file"}) {
		t.Fatalf("Fields = %v, want [file]", mod.Fields)
	}
	if mod.Before.File != "old.py" || mod.After.File != "new.py" {
		t.Fatalf("Before/After files: %q / %q", mod.Before.File, mod.After.File)
	}
}

func TestDiff_ToolMovedAndEdited_FieldsIncludesFileAndDescription(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/old.py": `@tool
def search(q: str): """Search."""
`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/new.py": `@tool
def search(q: str): """Search the web."""
`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Tools.Modified) != 1 {
		t.Fatalf("expected 1 Modified, got %+v", cs.Tools)
	}
	mod := cs.Tools.Modified[0]
	want := []string{"file", "description"}
	if !reflect.DeepEqual(mod.Fields, want) {
		t.Fatalf("Fields = %v, want %v", mod.Fields, want)
	}
	if mod.DescriptionDiff == nil {
		t.Fatal("expected DescriptionDiff to be attached to the move")
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

func TestDiff_AgentSystemPromptChange_AttachesTextDiff(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="m", system="You are a helpful assistant.")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="m", system="You are a very helpful assistant.")`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Agents.Modified) != 1 {
		t.Fatalf("Modified count: %+v", cs.Agents)
	}
	td := cs.Agents.Modified[0].SystemDiff
	if td == nil {
		t.Fatal("expected SystemDiff to be populated")
	}
	wantSegs := []TextSegment{
		{Kind: SegmentEqual, Text: "You are a "},
		{Kind: SegmentAdded, Text: "very "},
		{Kind: SegmentEqual, Text: "helpful assistant."},
	}
	if !reflect.DeepEqual(td.Segments, wantSegs) {
		t.Fatalf("segments: %+v", td.Segments)
	}
}

func TestDiff_DynamicSystemValue_NoTextDiffAttached(t *testing.T) {
	// Token diff requires both sides to be string literals. When one side
	// is a function call the diff stage records the change in Fields but
	// can't render a token-level delta — the consumer should see Fields=
	// [system] with SystemDiff=nil.
	base := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="m", system="static prompt")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="m", system=load_prompt("intro"))`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Agents.Modified) != 1 {
		t.Fatalf("Modified count: %+v", cs.Agents)
	}
	mod := cs.Agents.Modified[0]
	if !reflect.DeepEqual(mod.Fields, []string{"system"}) {
		t.Fatalf("Fields = %v", mod.Fields)
	}
	if mod.SystemDiff != nil {
		t.Fatalf("SystemDiff should be nil for dynamic value, got %+v", mod.SystemDiff)
	}
}

func TestDiff_ToolDescriptionChange_AttachesTextDiff(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/t.py": `@tool
def f(q: str):
    """Search."""
`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/t.py": `@tool
def f(q: str):
    """Search the web."""
`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Tools.Modified) != 1 {
		t.Fatalf("Modified count: %+v", cs.Tools)
	}
	td := cs.Tools.Modified[0].DescriptionDiff
	if td == nil {
		t.Fatal("expected DescriptionDiff to be populated")
	}
	wantSegs := []TextSegment{
		{Kind: SegmentEqual, Text: "Search"},
		{Kind: SegmentAdded, Text: " the web"},
		{Kind: SegmentEqual, Text: "."},
	}
	if !reflect.DeepEqual(td.Segments, wantSegs) {
		t.Fatalf("segments: %+v", td.Segments)
	}
}

func TestDiff_ModelOnlyChange_NoTextDiffAttached(t *testing.T) {
	// Sanity: Fields=[model] should not attach SystemDiff.
	base := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="sonnet", system="hi")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="opus", system="hi")`,
	}, "/repo")
	cs := Diff(base, head)
	if cs.Agents.Modified[0].SystemDiff != nil {
		t.Fatalf("SystemDiff should be nil when only model changed, got %+v",
			cs.Agents.Modified[0].SystemDiff)
	}
}

func TestDiff_NamedAgentsSurviveReorder(t *testing.T) {
	// Two agents in source order [A, B] in base, [B, A] in head.
	// Ordinal-keyed identity would flag two modifications; name-keyed
	// identity correctly reports zero changes.
	base := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(name="alpha", model="m1")
Agent(name="beta",  model="m2")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(name="beta",  model="m2")
Agent(name="alpha", model="m1")`,
	}, "/repo")
	cs := Diff(base, head)
	if !cs.IsEmpty() {
		t.Fatalf("expected no changes for named-agent reorder, got %+v", cs)
	}
}

func TestDiff_NamedAgentModelChangeAfterReorder(t *testing.T) {
	// Reorder + edit one named agent. Identity by name should produce
	// exactly one Modified entry (not the four-event mess that ordinal
	// keying would produce).
	base := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(name="alpha", model="sonnet")
Agent(name="beta",  model="opus")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(name="beta",  model="opus")
Agent(name="alpha", model="opus")`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Agents.Modified) != 1 || len(cs.Agents.Added) != 0 || len(cs.Agents.Removed) != 0 {
		t.Fatalf("expected 1 modification only, got %+v", cs.Agents)
	}
	mod := cs.Agents.Modified[0]
	if mod.Before.Name.Str != "alpha" || mod.After.Name.Str != "alpha" {
		t.Fatalf("expected the alpha agent modified, got before=%q after=%q",
			mod.Before.Name.Str, mod.After.Name.Str)
	}
	if !reflect.DeepEqual(mod.Fields, []string{"model"}) {
		t.Fatalf("Fields = %v, want [model]", mod.Fields)
	}
}

func TestDiff_AddingNameSurfacesAsRemovePlusAdd(t *testing.T) {
	// Documented edge case: editing a previously-unnamed agent to add an
	// explicit name flips identity from #ordinal to ::name. Diff
	// reports remove + add. This is the honest signal we accept.
	base := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(model="m")`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/a.py": `Agent(name="finally_named", model="m")`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Agents.Added) != 1 || len(cs.Agents.Removed) != 1 || len(cs.Agents.Modified) != 0 {
		t.Fatalf("expected exactly 1 add + 1 remove, got %+v", cs.Agents)
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
