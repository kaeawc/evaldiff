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

func TestRun_Coverage_PrintsTestCatalog(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "tests", "test_search.py"), `def test_one():
    pass

class TestCls:
    def test_method(self):
        pass
`)
	mustWrite(t, filepath.Join(dir, "src", "app.py"), `def regular(): pass`+"\n")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"coverage", dir}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	var got struct {
		Tests []struct {
			File, Name, Class string
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
}

func TestRun_Coverage_RequiresOneDirArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"coverage"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "usage:") {
		t.Fatalf("expected usage error, got %v", err)
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
