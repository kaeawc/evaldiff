package coverage

import (
	"context"
	"reflect"
	"testing"

	"github.com/kaeawc/evaldiff/internal/vfs"
)

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"test_search.py", true},
		{"sub/test_x.py", true},
		{"search_test.py", true},
		{"sub/search_test.py", true},
		{"conftest.py", false},
		{"sub/conftest.py", false},
		{"__init__.py", false},
		{"helpers.py", false},
		{"testing.py", false}, // doesn't match either pattern
		{"test.py", false},    // pytest needs the underscore
		{"_test.py", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsTestFile(tt.path); got != tt.want {
				t.Fatalf("IsTestFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestExtractTests(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []TestEntry
	}{
		{
			name: "top-level test functions",
			src: `def test_one():
    assert True

def test_two():
    assert False
`,
			want: []TestEntry{
				{File: "/x.py", Line: 1, Name: "test_one"},
				{File: "/x.py", Line: 4, Name: "test_two"},
			},
		},
		{
			name: "decorated test function reports decorator line",
			src: `@pytest.mark.parametrize("x", [1, 2])
def test_param(x):
    assert x
`,
			want: []TestEntry{
				{File: "/x.py", Line: 1, Name: "test_param", Identifiers: []string{"x"}},
			},
		},
		{
			name: "non-test functions ignored",
			src: `def helper():
    pass

def setup_module():
    pass

def test_real():
    pass
`,
			want: []TestEntry{
				{File: "/x.py", Line: 7, Name: "test_real"},
			},
		},
		{
			name: "test class with methods",
			src: `class TestSearch:
    def test_returns_results(self):
        pass

    def test_handles_empty(self):
        pass
`,
			want: []TestEntry{
				{File: "/x.py", Line: 2, Name: "test_returns_results", Class: "TestSearch"},
				{File: "/x.py", Line: 5, Name: "test_handles_empty", Class: "TestSearch"},
			},
		},
		{
			name: "decorated class method",
			src: `class TestSearch:
    @pytest.mark.slow
    def test_big(self):
        pass
`,
			want: []TestEntry{
				{File: "/x.py", Line: 2, Name: "test_big", Class: "TestSearch"},
			},
		},
		{
			name: "class with __init__ is skipped per pytest rules",
			src: `class TestBroken:
    def __init__(self):
        pass

    def test_method(self):
        pass
`,
			want: nil,
		},
		{
			name: "class without Test prefix is ignored",
			src: `class Helper:
    def test_method(self):
        pass
`,
			want: nil,
		},
		{
			name: "non-test methods inside test class ignored",
			src: `class TestSearch:
    def setup_method(self):
        pass

    def helper(self):
        pass

    def test_one(self):
        pass
`,
			want: []TestEntry{
				{File: "/x.py", Line: 8, Name: "test_one", Class: "TestSearch"},
			},
		},
		{
			name: "no tests → nil",
			src:  "x = 1\n",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractTests(context.Background(), "/x.py", []byte(tt.src))
			if err != nil {
				t.Fatalf("ExtractTests: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("mismatch:\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

func TestExtractTests_CollectsIdentifiersFromBody(t *testing.T) {
	src := `from app.agents import researcher
from app.tools import search

def test_research():
    result = researcher.run("hello")
    items = search(query="x")
    assert len(items) > 0

class TestX:
    def test_method(self):
        helper = make_helper()
        helper.go()
`
	got, err := ExtractTests(context.Background(), "/x.py", []byte(src))
	if err != nil {
		t.Fatalf("ExtractTests: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("test count = %d, want 2", len(got))
	}
	// test_research body references: researcher, run, items, search, query,
	// assert (keyword), len.
	wantTopLevel := []string{"items", "len", "query", "researcher", "result", "run", "search"}
	if !reflect.DeepEqual(got[0].Identifiers, wantTopLevel) {
		t.Fatalf("test_research Identifiers:\n got: %v\nwant: %v", got[0].Identifiers, wantTopLevel)
	}
	// test_method body references: helper, make_helper, go.
	// `self` is a parameter, not a body reference, so it's intentionally
	// not collected — collectBodyIdentifiers walks only the function body.
	wantMethod := []string{"go", "helper", "make_helper"}
	if !reflect.DeepEqual(got[1].Identifiers, wantMethod) {
		t.Fatalf("test_method Identifiers:\n got: %v\nwant: %v", got[1].Identifiers, wantMethod)
	}
}

func TestExtractTests_EmptyBodyHasNilIdentifiers(t *testing.T) {
	src := `def test_one():
    pass
`
	got, err := ExtractTests(context.Background(), "/x.py", []byte(src))
	if err != nil {
		t.Fatalf("ExtractTests: %v", err)
	}
	if got[0].Identifiers != nil {
		t.Fatalf("Identifiers = %v, want nil for `pass`-only body", got[0].Identifiers)
	}
}

func TestBuild_DiscoversTestsAcrossFiles(t *testing.T) {
	fs := vfs.NewMem(map[string]string{
		"/repo/tests/test_a.py": `def test_one():
    pass
`,
		"/repo/tests/sub/test_b.py": `class TestThing:
    def test_method(self):
        pass
`,
		"/repo/tests/conftest.py": `def fixture(): pass`,
		"/repo/src/app.py":        `def regular_function(): pass`,
		"/repo/tests/helpers.py":  `def test_should_be_ignored(): pass`,
	})

	cov, err := Build(context.Background(), fs, "/repo")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if cov.Root != "/repo" {
		t.Fatalf("Root = %q", cov.Root)
	}
	want := []TestEntry{
		{File: "tests/sub/test_b.py", Line: 2, Name: "test_method", Class: "TestThing"},
		{File: "tests/test_a.py", Line: 1, Name: "test_one"},
	}
	if !reflect.DeepEqual(cov.Tests, want) {
		t.Fatalf("tests mismatch:\n got: %+v\nwant: %+v", cov.Tests, want)
	}
}

func TestBuild_EmptyTree(t *testing.T) {
	cov, err := Build(context.Background(), vfs.NewMem(map[string]string{}), "/repo")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(cov.Tests) != 0 {
		t.Fatalf("expected zero tests, got %+v", cov.Tests)
	}
}

func TestBuild_NoTestFilesInTree(t *testing.T) {
	// Project has Python files but none match pytest's discovery rules.
	cov, err := Build(context.Background(), vfs.NewMem(map[string]string{
		"/repo/app/main.py":     `def run(): pass`,
		"/repo/app/__init__.py": "",
	}), "/repo")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(cov.Tests) != 0 {
		t.Fatalf("expected zero tests, got %+v", cov.Tests)
	}
}
