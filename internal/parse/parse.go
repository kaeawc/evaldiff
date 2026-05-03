// Package parse wraps tree-sitter-python with the small surface evaldiff
// needs: parse a source byte slice into a Tree, walk it, and clean up.
package parse

import (
	"context"
	"fmt"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/python"
)

// Result owns a parsed tree and the source it was parsed from. Callers must
// call Close when done; the underlying tree-sitter Tree holds C memory.
type Result struct {
	Source []byte
	Tree   *sitter.Tree
}

func (r *Result) Close() {
	if r != nil && r.Tree != nil {
		r.Tree.Close()
		r.Tree = nil
	}
}

// Root returns the tree's root node.
func (r *Result) Root() *sitter.Node {
	return r.Tree.RootNode()
}

// Text returns the source bytes covered by node.
func (r *Result) Text(node *sitter.Node) string {
	if node == nil {
		return ""
	}
	return node.Content(r.Source)
}

// Parse parses Python source. The returned Result must be Closed.
func Parse(ctx context.Context, src []byte) (*Result, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	tree, err := parser.ParseCtx(ctx, nil, src)
	if err != nil {
		return nil, fmt.Errorf("parse python: %w", err)
	}
	return &Result{Source: src, Tree: tree}, nil
}
