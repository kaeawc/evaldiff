package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRun_Index_PrintsBehaviorJSON(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.py"), `Agent(model="claude-sonnet-4-6")
`)
	mustWrite(t, filepath.Join(dir, "tools.py"), `@tool
def search(q: str):
    """Search."""
`)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"index", dir}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}

	var got struct {
		Files []struct {
			File   string
			Hash   string
			Agents []struct {
				Constructor string
				Model       struct{ Str string }
			}
			Tools []struct{ Name string }
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if len(got.Files) != 2 {
		t.Fatalf("Files: %+v", got.Files)
	}
	if got.Files[0].File != "a.py" || got.Files[0].Agents[0].Model.Str != "claude-sonnet-4-6" {
		t.Fatalf("a.py entry: %+v", got.Files[0])
	}
	if got.Files[1].File != "tools.py" || got.Files[1].Tools[0].Name != "search" {
		t.Fatalf("tools.py entry: %+v", got.Files[1])
	}
}

func TestRun_IndexHash_PrintsOnlyHash(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "x.py"), `Agent(model="m")`+"\n")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"index", "--hash", dir}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	out := strings.TrimSpace(stdout.String())
	if len(out) != 64 {
		t.Fatalf("expected 64-char hex hash, got %q", out)
	}
}

func TestRun_Index_RequiresExactlyOneDirArg(t *testing.T) {
	cases := [][]string{
		{"index"},
		{"index", "/tmp", "/extra"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		err := run(args, &stdout, &stderr)
		if err == nil {
			t.Fatalf("args=%v: expected error", args)
		}
		if !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("args=%v: error %q lacks 'usage:'", args, err)
		}
	}
}

func TestRun_Diff_PrintsChangesetJSON(t *testing.T) {
	baseDir := t.TempDir()
	headDir := t.TempDir()
	mustWrite(t, filepath.Join(baseDir, "a.py"), `Agent(model="sonnet")`+"\n")
	mustWrite(t, filepath.Join(headDir, "a.py"), `Agent(model="opus")`+"\n")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"diff", baseDir, headDir}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	var got struct {
		Agents struct {
			Modified []struct {
				Fields []string
				Before struct{ Model struct{ Str string } }
				After  struct{ Model struct{ Str string } }
			}
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if len(got.Agents.Modified) != 1 {
		t.Fatalf("Modified count = %d, want 1; output: %s", len(got.Agents.Modified), stdout.String())
	}
	mod := got.Agents.Modified[0]
	if !reflect.DeepEqual(mod.Fields, []string{"model"}) {
		t.Fatalf("Fields = %v, want [model]", mod.Fields)
	}
	if mod.Before.Model.Str != "sonnet" || mod.After.Model.Str != "opus" {
		t.Fatalf("model before/after: %q / %q", mod.Before.Model.Str, mod.After.Model.Str)
	}
}

func TestRun_Coverage_PrintsTestCatalogWithTouches(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "tools.py"), `from claude_agent_sdk import tool
@tool
def search(q: str): """Search."""
`)
	mustWrite(t, filepath.Join(dir, "tests", "test_search.py"), `from app.tools import search

def test_one():
    search("q")

class TestCls:
    def test_method(self):
        search("q")
`)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"coverage", dir}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	var got struct {
		Tests []struct {
			File, Name, Class string
			Touches           []struct{ Kind, File, Name string }
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if len(got.Tests) != 2 {
		t.Fatalf("Tests count = %d, want 2; output: %s", len(got.Tests), stdout.String())
	}
	if got.Tests[0].Name != "test_one" || got.Tests[0].Class != "" {
		t.Fatalf("first: %+v", got.Tests[0])
	}
	if got.Tests[1].Name != "test_method" || got.Tests[1].Class != "TestCls" {
		t.Fatalf("second: %+v", got.Tests[1])
	}
	for i, te := range got.Tests {
		if len(te.Touches) != 1 || te.Touches[0].Name != "search" {
			t.Fatalf("test %d Touches = %+v, want [search]", i, te.Touches)
		}
	}
}

func TestRun_Coverage_NoTouchesFlag(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "app", "tools.py"), `from claude_agent_sdk import tool
@tool
def search(q: str): """Search."""
`)
	mustWrite(t, filepath.Join(dir, "tests", "test_x.py"), `from app.tools import search

def test_one():
    search("q")
`)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"coverage", "--no-touches", dir}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	var got struct {
		Tests []struct {
			Touches []struct{ Name string }
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if len(got.Tests) != 1 || len(got.Tests[0].Touches) != 0 {
		t.Fatalf("expected empty Touches, got %+v", got.Tests)
	}
}

func TestRun_Coverage_RequiresOneDirArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"coverage"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRun_Risk_HeadlineShorthandReturnsAtRiskTests(t *testing.T) {
	baseDir := t.TempDir()
	headDir := t.TempDir()
	mustWrite(t, filepath.Join(baseDir, "app", "agents.py"), `from agents import Agent
researcher = Agent(name="researcher", model="sonnet", instructions="research")
`)
	mustWrite(t, filepath.Join(headDir, "app", "agents.py"), `from agents import Agent
researcher = Agent(name="researcher", model="opus", instructions="research")
`)
	// The test file goes only in head — that's the source of HEAD coverage.
	mustWrite(t, filepath.Join(headDir, "tests", "test_pipeline.py"), `from app.agents import researcher

def test_research():
    researcher.run("hi")

def test_unrelated():
    print("nothing")
`)
	// Match files between base and head tree shapes so paths line up.
	mustWrite(t, filepath.Join(baseDir, "tests", "test_pipeline.py"), `from app.agents import researcher

def test_research():
    researcher.run("hi")

def test_unrelated():
    print("nothing")
`)

	var stdout, stderr bytes.Buffer
	if err := run([]string{baseDir, headDir}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	var got struct {
		Tests []struct {
			Test struct {
				Name string `json:"name"`
				File string `json:"file"`
			} `json:"test"`
			Affected []struct {
				Ref struct {
					Kind string `json:"kind"`
					Name string `json:"name"`
				} `json:"ref"`
				Kind  string  `json:"kind"`
				Score float64 `json:"score"`
			} `json:"affected"`
			Score float64 `json:"score"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	// Per-test refinement (V7c): only test_research references the
	// researcher agent; test_unrelated has no relevant identifiers and
	// is correctly excluded from the at-risk set.
	if len(got.Tests) != 1 {
		t.Fatalf("Tests count = %d, want 1 (only test_research references researcher)", len(got.Tests))
	}
	if got.Tests[0].Test.Name != "test_research" {
		t.Fatalf("at-risk test = %s, want test_research", got.Tests[0].Test.Name)
	}
	if got.Tests[0].Score != 1.0 || got.Tests[0].Affected[0].Ref.Name != "researcher" {
		t.Fatalf("first test entry: %+v", got.Tests[0])
	}
}

func TestRun_Risk_ExplicitSubcommandWorks(t *testing.T) {
	baseDir := t.TempDir()
	headDir := t.TempDir()
	src := `from agents import Agent
a = Agent(name="a", model="m", instructions="x")
`
	mustWrite(t, filepath.Join(baseDir, "x.py"), src)
	mustWrite(t, filepath.Join(headDir, "x.py"), src)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"risk", baseDir, headDir}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	// No changes → empty result, but must be valid JSON.
	if !json.Valid(stdout.Bytes()) {
		t.Fatalf("not valid JSON: %s", stdout.String())
	}
}

func TestRun_Risk_MarkdownFormat(t *testing.T) {
	baseDir := t.TempDir()
	headDir := t.TempDir()
	mustWrite(t, filepath.Join(baseDir, "app", "agents.py"), `from agents import Agent
researcher = Agent(name="researcher", model="sonnet", instructions="research")
`)
	mustWrite(t, filepath.Join(headDir, "app", "agents.py"), `from agents import Agent
researcher = Agent(name="researcher", model="opus", instructions="research")
`)
	mustWrite(t, filepath.Join(headDir, "tests", "test_pipeline.py"), `from app.agents import researcher
def test_research():
    researcher.run("hi")
`)
	mustWrite(t, filepath.Join(baseDir, "tests", "test_pipeline.py"), `from app.agents import researcher
def test_research():
    researcher.run("hi")
`)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"risk", "--format", "markdown", baseDir, headDir}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"### evaldiff:",
		"#### `tests/test_pipeline.py::test_research`",
		"**modified** agent `researcher`",
		"fields: `model`",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRun_Risk_UnknownFormatErrors(t *testing.T) {
	baseDir := t.TempDir()
	headDir := t.TempDir()
	mustWrite(t, filepath.Join(baseDir, "x.py"), "x = 1\n")
	mustWrite(t, filepath.Join(headDir, "x.py"), "x = 1\n")

	var stdout, stderr bytes.Buffer
	err := run([]string{"risk", "--format", "yaml", baseDir, headDir}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unknown --format") {
		t.Fatalf("expected unknown-format error, got %v", err)
	}
}

func TestRun_Risk_RequiresTwoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"risk", "/tmp"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRun_HeadlineShorthand_TyposSurfaceAsErrors(t *testing.T) {
	// `evaldiff indxe foo` (typo for `index`) gets caught by the
	// 2-arg risk shorthand and tries to use "indxe" as a base
	// directory. The user sees a clear filesystem error rather than
	// silent nonsense — that's the contract.
	var stdout, stderr bytes.Buffer
	err := run([]string{"indxe", "foo"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Either "unknown command" or a filesystem error from the index
	// build is acceptable; the important contract is the user sees
	// a clear error rather than silent success.
	if !strings.Contains(err.Error(), "unknown command") &&
		!strings.Contains(err.Error(), "no such file") &&
		!strings.Contains(err.Error(), "lstat") {
		t.Fatalf("error %q should surface as command-or-filesystem error", err)
	}
}

func TestRun_Diff_RequiresTwoDirArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"diff", "/tmp"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got %v", err)
	}
}

func TestRun_NoArgs_ShowsUsageAndErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(nil, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error when no args")
	}
	if !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("stderr should include usage, got %q", stderr.String())
	}
}

func TestRun_UnknownCommand_Errors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"nope"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("error %q lacks 'unknown command'", err)
	}
}

func TestRun_Version_PrintsVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--version"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(stdout.String()) != version {
		t.Fatalf("version = %q, want %q", stdout.String(), version)
	}
}

func TestRun_Help_PrintsUsageWithoutError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"help"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), "usage:") {
		t.Fatalf("stdout lacks usage banner: %q", stdout.String())
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
