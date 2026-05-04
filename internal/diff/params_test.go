package diff

import (
	"reflect"
	"testing"

	"github.com/kaeawc/evaldiff/internal/index"
)

func TestParamsStructuralDiff_AddedAndRemoved(t *testing.T) {
	before := []index.ToolParam{
		{Name: "q", Annotation: dynVal("str")},
		{Name: "limit", Annotation: dynVal("int"), HasDefault: true},
	}
	after := []index.ToolParam{
		{Name: "q", Annotation: dynVal("str")},
		{Name: "lang", Annotation: dynVal("str"), HasDefault: true},
	}
	got := paramsStructuralDiff(before, after)
	if !reflect.DeepEqual(paramNames(got.Added), []string{"lang"}) {
		t.Fatalf("Added: %+v", got.Added)
	}
	if !reflect.DeepEqual(paramNames(got.Removed), []string{"limit"}) {
		t.Fatalf("Removed: %+v", got.Removed)
	}
	if len(got.Modified) != 0 {
		t.Fatalf("Modified should be empty, got %+v", got.Modified)
	}
}

func TestParamsStructuralDiff_AnnotationChange(t *testing.T) {
	before := []index.ToolParam{{Name: "q", Annotation: dynVal("str")}}
	after := []index.ToolParam{{Name: "q", Annotation: dynVal("bytes")}}
	got := paramsStructuralDiff(before, after)
	if len(got.Modified) != 1 {
		t.Fatalf("Modified len = %d", len(got.Modified))
	}
	if !reflect.DeepEqual(got.Modified[0].Fields, []string{"annotation"}) {
		t.Fatalf("Fields = %v", got.Modified[0].Fields)
	}
	if got.Modified[0].After.Annotation.Source != "bytes" {
		t.Fatalf("After.Annotation = %+v", got.Modified[0].After.Annotation)
	}
}

func TestParamsStructuralDiff_DefaultGained(t *testing.T) {
	before := []index.ToolParam{{Name: "q", Annotation: dynVal("str")}}
	after := []index.ToolParam{{Name: "q", Annotation: dynVal("str"), HasDefault: true}}
	got := paramsStructuralDiff(before, after)
	if len(got.Modified) != 1 || !reflect.DeepEqual(got.Modified[0].Fields, []string{"has_default"}) {
		t.Fatalf("Modified: %+v", got.Modified)
	}
}

func TestParamsStructuralDiff_AnnotationAndDefaultBothChanged(t *testing.T) {
	before := []index.ToolParam{{Name: "q", Annotation: dynVal("str")}}
	after := []index.ToolParam{{Name: "q", Annotation: dynVal("bytes"), HasDefault: true}}
	got := paramsStructuralDiff(before, after)
	if len(got.Modified) != 1 ||
		!reflect.DeepEqual(got.Modified[0].Fields, []string{"annotation", "has_default"}) {
		t.Fatalf("Fields: %v", got.Modified[0].Fields)
	}
}

func TestParamsStructuralDiff_ReorderOnly_IsEmpty(t *testing.T) {
	// Reorder is intentionally invisible to per-name structural diff.
	// The parent ToolMod still flags Fields=[params] because Python
	// signature ordering changed.
	before := []index.ToolParam{
		{Name: "q", Annotation: dynVal("str")},
		{Name: "limit", Annotation: dynVal("int"), HasDefault: true},
	}
	after := []index.ToolParam{
		{Name: "limit", Annotation: dynVal("int"), HasDefault: true},
		{Name: "q", Annotation: dynVal("str")},
	}
	got := paramsStructuralDiff(before, after)
	if !got.IsEmpty() {
		t.Fatalf("expected empty diff for reorder, got %+v", got)
	}
}

func TestDiff_ToolParamAdded_AttachesParamsDiff(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/t.py": `@tool
def f(q: str):
    """d."""
`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/t.py": `@tool
def f(q: str, limit: int = 10):
    """d."""
`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Tools.Modified) != 1 {
		t.Fatalf("Modified: %+v", cs.Tools)
	}
	pd := cs.Tools.Modified[0].ParamsDiff
	if pd == nil {
		t.Fatal("expected ParamsDiff")
	}
	if !reflect.DeepEqual(paramNames(pd.Added), []string{"limit"}) {
		t.Fatalf("Added: %+v", pd.Added)
	}
}

func TestDiff_ToolParamReorder_ParamsDiffEmpty(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/t.py": `@tool
def f(q: str, limit: int = 10):
    """d."""
`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/t.py": `@tool
def f(limit: int = 10, q: str = "x"):
    """d."""
`,
	}, "/repo")
	cs := Diff(base, head)
	if len(cs.Tools.Modified) != 1 {
		t.Fatalf("Modified: %+v", cs.Tools)
	}
	mod := cs.Tools.Modified[0]
	if !containsField(mod.Fields, "params") {
		t.Fatalf("Fields should include params, got %v", mod.Fields)
	}
	if mod.ParamsDiff == nil {
		t.Fatal("ParamsDiff should be non-nil even for reorder-only edits")
	}
	// q gained a default in this fixture, so we expect one Modified entry,
	// not zero — confirming structural diff still catches per-name changes
	// even when the reorder confuses positional comparison.
	if len(mod.ParamsDiff.Modified) != 1 || mod.ParamsDiff.Modified[0].Before.Name != "q" {
		t.Fatalf("expected 1 modified param 'q' (default gained), got %+v", mod.ParamsDiff.Modified)
	}
}

func TestDiff_ToolNoParamFieldChange_ParamsDiffNil(t *testing.T) {
	base := buildIdx(t, map[string]string{
		"/repo/t.py": `@tool
def f(q: str):
    """before."""
`,
	}, "/repo")
	head := buildIdx(t, map[string]string{
		"/repo/t.py": `@tool
def f(q: str):
    """after."""
`,
	}, "/repo")
	cs := Diff(base, head)
	if cs.Tools.Modified[0].ParamsDiff != nil {
		t.Fatalf("ParamsDiff should be nil when params didn't change, got %+v",
			cs.Tools.Modified[0].ParamsDiff)
	}
}

func dynVal(src string) index.Value {
	return index.Value{Kind: index.ValueDynamic, Source: src}
}

func paramNames(ps []index.ToolParam) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}
