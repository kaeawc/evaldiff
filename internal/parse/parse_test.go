package parse

import (
	"context"
	"strings"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
)

func TestParse_RootIsModule(t *testing.T) {
	r, err := Parse(context.Background(), []byte("x = 1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer r.Close()

	got := r.Root().Type()
	if got != "module" {
		t.Fatalf("root type = %q, want %q", got, "module")
	}
}

func TestParse_TextRoundtripsLeafNode(t *testing.T) {
	src := []byte(`name = "hello world"`)
	r, err := Parse(context.Background(), src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	defer r.Close()

	stringNode := findFirst(r.Root(), "string")
	if stringNode == nil {
		t.Fatal("no string node found")
	}
	got := r.Text(stringNode)
	if !strings.Contains(got, "hello world") {
		t.Fatalf("Text = %q, want it to contain %q", got, "hello world")
	}
}

func TestParse_RecoversFromSyntaxError(t *testing.T) {
	// tree-sitter is error-tolerant; broken Python should still produce a tree.
	r, err := Parse(context.Background(), []byte("def broken(:\n"))
	if err != nil {
		t.Fatalf("Parse returned err for broken input: %v", err)
	}
	defer r.Close()
	if r.Root() == nil {
		t.Fatal("nil root for broken input")
	}
	if !r.Root().HasError() {
		t.Fatal("expected HasError() = true for broken input")
	}
}

func TestParse_CtxCancelDoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// We don't assert error vs success — small inputs may finish before the
	// context check. The contract under test is "doesn't panic".
	r, err := Parse(ctx, []byte("x = 1"))
	if err == nil {
		r.Close()
	}
}

// findFirst returns the first descendant (depth-first, including n) whose
// Type() matches kind, or nil.
func findFirst(n *sitter.Node, kind string) *sitter.Node {
	if n == nil {
		return nil
	}
	if n.Type() == kind {
		return n
	}
	for i := uint32(0); i < n.ChildCount(); i++ {
		if hit := findFirst(n.Child(int(i)), kind); hit != nil {
			return hit
		}
	}
	return nil
}
