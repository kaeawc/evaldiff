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

func TestImportResolver_SrcLayout(t *testing.T) {
	// Project layout: package lives at src/agents/ and tests import
	// `from agents import Agent` per PEP 517 / src-layout convention.
	idx := &index.Index{Files: []index.FileEntry{
		{File: "src/agents/__init__.py"},
		{File: "src/agents/runner.py"},
		{File: "src/agents/tools/search.py"},
		{File: "tests/test_runner.py"},
	}}
	r := newImportResolver(idx)
	tests := []struct {
		module string
		want   string
	}{
		{"agents", "src/agents/__init__.py"},
		{"agents.runner", "src/agents/runner.py"},
		{"agents.tools.search", "src/agents/tools/search.py"},
		{"missing", ""},
	}
	for _, tt := range tests {
		t.Run(tt.module, func(t *testing.T) {
			if got := r.resolve(tt.module); got != tt.want {
				t.Fatalf("resolve(%q) = %q, want %q", tt.module, got, tt.want)
			}
		})
	}
}

func TestImportResolver_RootShadowsSrc(t *testing.T) {
	// If a module exists both at root and under src/, root wins. This
	// matches Python's actual sys.path order when a project still has a
	// top-level package directory alongside src/.
	idx := &index.Index{Files: []index.FileEntry{
		{File: "shared.py"},
		{File: "src/shared.py"},
	}}
	r := newImportResolver(idx)
	if got := r.resolve("shared"); got != "shared.py" {
		t.Fatalf("resolve(shared) = %q, want shared.py (root shadows src/)", got)
	}
}

func TestImportResolver_NoSrcDir_NoExtraCandidates(t *testing.T) {
	// Sanity: when the tree has no src/ at all, srcRoots stays empty and
	// resolution behaves identically to the pre-V2 behavior.
	idx := &index.Index{Files: []index.FileEntry{
		{File: "app/foo.py"},
	}}
	r := newImportResolver(idx)
	if len(r.srcRoots) != 0 {
		t.Fatalf("srcRoots = %v, want empty", r.srcRoots)
	}
	if got := r.resolve("foo"); got != "" {
		t.Fatalf("resolve(foo) = %q, want empty (no src/)", got)
	}
}

func TestAttachTouches_SrcLayoutE2E(t *testing.T) {
	// End-to-end: a test under tests/ imports `from agents import` and
	// the package lives at src/agents/. Touches must populate.
	fs := vfs.NewMem(map[string]string{
		"/repo/src/agents/__init__.py": `from agents import function_tool

@function_tool
def search(q: str): """Search."""
`,
		"/repo/tests/test_search.py": `from agents import search

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
		{Kind: "tool", File: "src/agents/__init__.py", Name: "search"},
	}
	if !reflect.DeepEqual(cov.Tests[0].Touches, want) {
		t.Fatalf("Touches:\n got: %+v\nwant: %+v", cov.Tests[0].Touches, want)
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
