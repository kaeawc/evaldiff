package coverage

import (
	"context"
	"reflect"
	"testing"

	"github.com/kaeawc/evaldiff/internal/index"
	"github.com/kaeawc/evaldiff/internal/vfs"
)

func TestImportResolver(t *testing.T) {
	idx := &index.Index{Files: []index.FileEntry{
		{File: "app/agents.py"},
		{File: "app/tools/__init__.py"},
		{File: "app/tools/search.py"},
		{File: "lib/util.py"},
	}}
	r := newImportResolver(idx)
	tests := []struct {
		module string
		want   string
	}{
		{"app.agents", "app/agents.py"},
		{"app.tools", "app/tools/__init__.py"}, // package init wins
		{"app.tools.search", "app/tools/search.py"},
		{"lib.util", "lib/util.py"},
		{"missing", ""},
		{".relative", ""}, // relative imports not resolved today
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.module, func(t *testing.T) {
			if got := r.resolve(tt.module); got != tt.want {
				t.Fatalf("resolve(%q) = %q, want %q", tt.module, got, tt.want)
			}
		})
	}
}

func TestAttachTouches_FileLevelMappingFromImports(t *testing.T) {
	fs := vfs.NewMem(map[string]string{
		"/repo/app/agents.py": `from claude_agent_sdk import Agent
researcher = Agent(model="m", system="research")
writer = Agent(model="m", system="write")
`,
		"/repo/app/tools.py": `from claude_agent_sdk import tool
@tool
def search(q: str): """Search."""
@tool
def browse(url: str): """Browse."""
`,
		"/repo/tests/test_pipeline.py": `from app.agents import researcher
from app.tools import search

def test_one(): pass
def test_two(): pass
`,
		"/repo/tests/test_other.py": `def test_unrelated(): pass
`,
	})
	idx, err := index.Build(context.Background(), fs, "/repo")
	if err != nil {
		t.Fatalf("index.Build: %v", err)
	}
	cov, err := Build(context.Background(), fs, "/repo")
	if err != nil {
		t.Fatalf("coverage.Build: %v", err)
	}
	if err := AttachTouches(context.Background(), fs, cov, idx); err != nil {
		t.Fatalf("AttachTouches: %v", err)
	}

	wantPipelineTouches := []BehaviorRef{
		{Kind: "agent", File: "app/agents.py", Name: "Agent#0"},
		{Kind: "agent", File: "app/agents.py", Name: "Agent#1"},
		{Kind: "tool", File: "app/tools.py", Name: "browse"},
		{Kind: "tool", File: "app/tools.py", Name: "search"},
	}
	if !reflect.DeepEqual(cov.Tests[1].Touches, wantPipelineTouches) {
		t.Fatalf("test_one (idx 1) Touches:\n got: %+v\nwant: %+v", cov.Tests[1].Touches, wantPipelineTouches)
	}
	if !reflect.DeepEqual(cov.Tests[2].Touches, wantPipelineTouches) {
		t.Fatalf("test_two (idx 2) Touches did not match test_one (file-coarse mapping):\n got: %+v\nwant: %+v",
			cov.Tests[2].Touches, wantPipelineTouches)
	}
	if cov.Tests[0].Touches != nil {
		t.Fatalf("test_unrelated should have no touches, got %+v", cov.Tests[0].Touches)
	}
}

func TestAttachTouches_UnresolvedImportsAreSilentlySkipped(t *testing.T) {
	// A test imports a third-party package and a non-existent local
	// module. AttachTouches must not error; Touches just stays empty.
	fs := vfs.NewMem(map[string]string{
		"/repo/app/agents.py": `Agent(model="m")
`,
		"/repo/tests/test_x.py": `import third_party_lib
from missing_local import thing

def test_one(): pass
`,
	})
	idx, err := index.Build(context.Background(), fs, "/repo")
	if err != nil {
		t.Fatalf("index.Build: %v", err)
	}
	cov, err := Build(context.Background(), fs, "/repo")
	if err != nil {
		t.Fatalf("coverage.Build: %v", err)
	}
	if err := AttachTouches(context.Background(), fs, cov, idx); err != nil {
		t.Fatalf("AttachTouches: %v", err)
	}
	if cov.Tests[0].Touches != nil {
		t.Fatalf("expected no touches for unresolved imports, got %+v", cov.Tests[0].Touches)
	}
}

func TestAttachTouches_PackageInitImport(t *testing.T) {
	fs := vfs.NewMem(map[string]string{
		"/repo/app/tools/__init__.py": `from claude_agent_sdk import tool
@tool
def search(q: str): """Search."""
`,
		"/repo/tests/test_x.py": `from app import tools

def test_one(): pass
`,
	})
	idx, err := index.Build(context.Background(), fs, "/repo")
	if err != nil {
		t.Fatalf("index.Build: %v", err)
	}
	cov, err := Build(context.Background(), fs, "/repo")
	if err != nil {
		t.Fatalf("coverage.Build: %v", err)
	}
	if err := AttachTouches(context.Background(), fs, cov, idx); err != nil {
		t.Fatalf("AttachTouches: %v", err)
	}
	want := []BehaviorRef{
		{Kind: "tool", File: "app/tools/__init__.py", Name: "search"},
	}
	if !reflect.DeepEqual(cov.Tests[0].Touches, want) {
		t.Fatalf("Touches:\n got: %+v\nwant: %+v", cov.Tests[0].Touches, want)
	}
}

func TestAttachTouches_NilInputsAreNoOps(t *testing.T) {
	if err := AttachTouches(context.Background(), nil, nil, nil); err != nil {
		t.Fatalf("nil inputs should be no-op, got %v", err)
	}
	cov := &Coverage{}
	if err := AttachTouches(context.Background(), nil, cov, nil); err != nil {
		t.Fatalf("nil idx should be no-op, got %v", err)
	}
	if err := AttachTouches(context.Background(), nil, nil, &index.Index{}); err != nil {
		t.Fatalf("nil cov should be no-op, got %v", err)
	}
}

func TestDedupeRefs_StableSortAndDedupe(t *testing.T) {
	in := []BehaviorRef{
		{Kind: "tool", File: "z.py", Name: "z_tool"},
		{Kind: "agent", File: "a.py", Name: "Agent#0"},
		{Kind: "tool", File: "a.py", Name: "search"},
		{Kind: "tool", File: "z.py", Name: "z_tool"}, // dup
	}
	got := dedupeRefs(in)
	want := []BehaviorRef{
		{Kind: "agent", File: "a.py", Name: "Agent#0"},
		{Kind: "tool", File: "a.py", Name: "search"},
		{Kind: "tool", File: "z.py", Name: "z_tool"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dedupeRefs:\n got: %+v\nwant: %+v", got, want)
	}
}
