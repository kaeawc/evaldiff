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
	// the package lives at src/agents/. Touches must populate when the
	// test body actually references the imported name.
	fs := vfs.NewMem(map[string]string{
		"/repo/src/agents/__init__.py": `from agents import function_tool

@function_tool
def search(q: str): """Search."""
`,
		"/repo/tests/test_search.py": `from agents import search

def test_one():
    search("hello")
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

func TestAttachTouches_PerTestRefinementByIdentifierUse(t *testing.T) {
	// Per-test refinement (V7c) is "imports the test uses, file-coarse on
	// the other side": referencing any name an import bound pulls in
	// every behavior from that import's target file. This catches the
	// "test calls a helper that internally uses agents" pattern.
	//
	// What's filtered: imports the test doesn't reference at all (the
	// "test_unrelated" case below). What's still pulled in together:
	// sibling behaviors in the same file (writer alongside researcher).
	fs := vfs.NewMem(map[string]string{
		"/repo/app/agents.py": `from agents import Agent
researcher = Agent(name="researcher", model="m", instructions="r")
writer     = Agent(name="writer",     model="m", instructions="w")
`,
		"/repo/app/tools.py": `from agents import function_tool
@function_tool
def search(q: str): """Search."""
@function_tool
def browse(url: str): """Browse."""
`,
		"/repo/tests/test_pipeline.py": `from app.agents import researcher, writer
from app.tools  import search, browse

def test_uses_agents_only():
    researcher.run("hi")

def test_uses_tools_only():
    search("q")

def test_unrelated():
    print("nothing imported referenced")
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
	agentsFile := []BehaviorRef{
		{Kind: "agent", File: "app/agents.py", Name: "researcher"},
		{Kind: "agent", File: "app/agents.py", Name: "writer"},
	}
	toolsFile := []BehaviorRef{
		{Kind: "tool", File: "app/tools.py", Name: "browse"},
		{Kind: "tool", File: "app/tools.py", Name: "search"},
	}
	wantByName := map[string][]BehaviorRef{
		// Reference a name from app.agents → every agent in that file.
		"test_uses_agents_only": agentsFile,
		// Reference a name from app.tools → every tool in that file.
		"test_uses_tools_only": toolsFile,
		// References nothing imported → no touches at all.
		"test_unrelated": nil,
	}
	for _, te := range cov.Tests {
		want, ok := wantByName[te.Name]
		if !ok {
			t.Fatalf("unexpected test %q", te.Name)
		}
		if !reflect.DeepEqual(te.Touches, want) {
			t.Fatalf("%s Touches:\n got: %+v\nwant: %+v", te.Name, te.Touches, want)
		}
	}
}

func TestAttachTouches_HelperImportPullsInColocatedAgents(t *testing.T) {
	// The motivating real-world case: a test imports a helper function
	// from a file that also contains agents. The test references the
	// helper, not the agent — but if the agent changes, the helper might
	// break, so the test should be at risk.
	fs := vfs.NewMem(map[string]string{
		"/repo/tests/fixtures.py": `from agents import Agent

bot = Agent(name="bot", model="m", instructions="x")

def run_bot(msg):
    return bot.run(msg)
`,
		"/repo/tests/test_helper.py": `from tests.fixtures import run_bot

def test_via_helper():
    result = run_bot("hi")
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
		{Kind: "agent", File: "tests/fixtures.py", Name: "bot"},
	}
	for _, te := range cov.Tests {
		if te.Name == "test_via_helper" {
			if !reflect.DeepEqual(te.Touches, want) {
				t.Fatalf("test_via_helper Touches:\n got: %+v\nwant: %+v", te.Touches, want)
			}
			return
		}
	}
	t.Fatal("test_via_helper not found in coverage")
}

func TestAttachTouches_UnresolvedImportsAreSilentlySkipped(t *testing.T) {
	// A test imports a third-party package and a non-existent local
	// module. AttachTouches must not error; Touches just stays empty.
	fs := vfs.NewMem(map[string]string{
		"/repo/app/agents.py": `Agent(model="m")
`,
		"/repo/tests/test_x.py": `import third_party_lib
from missing_local import thing

def test_one():
    third_party_lib.do()
    thing()
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

def test_one():
    tools.search("q")
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

func TestAttachTouches_NamedAgentRefUsesLiteralName(t *testing.T) {
	// When an Agent has a literal `name` kwarg the BehaviorRef Name
	// surfaces it instead of the constructor#ordinal fallback, giving
	// PR-comment renderers a human-readable identifier.
	fs := vfs.NewMem(map[string]string{
		"/repo/app/agents.py": `from agents import Agent
researcher = Agent(name="researcher", model="m", instructions="research")
writer     = Agent(model="m", instructions="write")
`,
		"/repo/tests/test_x.py": `from app.agents import researcher
def test_one():
    researcher.run("hi")
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
	// Per V7c file-coarse-on-touches: referencing `researcher` pulls in
	// the literal-named agent AND the unnamed sibling in the same file.
	want := []BehaviorRef{
		{Kind: "agent", File: "app/agents.py", Name: "Agent#1"},
		{Kind: "agent", File: "app/agents.py", Name: "researcher"},
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
