package index

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/kaeawc/evaldiff/internal/parse"
)

// Agent describes one statically-discovered agent constructor invocation in
// a Python source file (e.g. `Agent(model="claude-sonnet-4-6", ...)`).
// Every kwarg evaldiff cares about lives here; absent kwargs are
// ValueMissing so downstream stages can distinguish "unset" from "set to
// the empty string".
type Agent struct {
	File        string `json:"file"`        // file path passed to ExtractAgents
	Line        int    `json:"line"`        // 1-based line of the call expression
	Constructor string `json:"constructor"` // identifier or attribute text, e.g. "Agent" or "claude.Agent"
	Model       Value  `json:"model"`
	System      Value  `json:"system"`
	Tools       Value  `json:"tools"`
}

// ExtractAgents finds every Agent(...) (or qualified .Agent(...)) call in
// src and returns one record per call. The file argument is used only for
// the File field on results; src is the actual source bytes.
func ExtractAgents(ctx context.Context, file string, src []byte) ([]Agent, error) {
	r, err := parse.Parse(ctx, src)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var out []Agent
	walk(r.Root(), func(n *sitter.Node) {
		if n.Type() != "call" {
			return
		}
		fn := n.ChildByFieldName("function")
		name := nodeText(fn, src)
		if !isAgentCallee(name) {
			return
		}
		a := Agent{
			File:        file,
			Line:        int(n.StartPoint().Row) + 1,
			Constructor: name,
		}
		if argList := n.ChildByFieldName("arguments"); argList != nil {
			forEachKwarg(argList, src, func(kw string, value Value) {
				switch kw {
				case "model":
					a.Model = value
				case "system", "system_prompt", "instructions":
					if a.System.IsMissing() {
						a.System = value
					}
				case "tools":
					a.Tools = value
				}
			})
		}
		out = append(out, a)
	})
	return out, nil
}

// isAgentCallee returns true for `Agent` and any attribute access ending in
// `.Agent` (e.g. `claude.Agent`, `claude_agent_sdk.Agent`).
func isAgentCallee(name string) bool {
	if name == "Agent" {
		return true
	}
	return strings.HasSuffix(name, ".Agent")
}

// forEachKwarg invokes fn for every keyword_argument under argList,
// extracting the kwarg name and a Value describing its argument.
func forEachKwarg(argList *sitter.Node, src []byte, fn func(name string, v Value)) {
	for i := uint32(0); i < argList.NamedChildCount(); i++ {
		arg := argList.NamedChild(int(i))
		if arg.Type() != "keyword_argument" {
			continue
		}
		nameNode := arg.ChildByFieldName("name")
		valueNode := arg.ChildByFieldName("value")
		if nameNode == nil || valueNode == nil {
			continue
		}
		name := nodeText(nameNode, src)
		fn(name, valueFromNode(valueNode, src))
	}
}

// valueFromNode resolves a tree-sitter value node into a Value. String
// literals become ValueLiteral with their interior content; everything else
// is ValueDynamic with the raw source text preserved.
func valueFromNode(n *sitter.Node, src []byte) Value {
	source := nodeText(n, src)
	if n.Type() == "string" {
		return Value{Kind: ValueLiteral, Str: stringContent(n, src), Source: source}
	}
	return Value{Kind: ValueDynamic, Source: source}
}

// stringContent extracts the body of a Python string literal node, joining
// every string_content child. Triple-quoted, f-prefixed (without
// interpolations), and concatenated adjacent strings all collapse into one
// payload — good enough for prompt-text comparison.
func stringContent(n *sitter.Node, src []byte) string {
	var b strings.Builder
	var walkChildren func(node *sitter.Node)
	walkChildren = func(node *sitter.Node) {
		for i := uint32(0); i < node.ChildCount(); i++ {
			c := node.Child(int(i))
			if c.Type() == "string_content" {
				b.WriteString(nodeText(c, src))
			} else if c.NamedChildCount() > 0 {
				walkChildren(c)
			}
		}
	}
	walkChildren(n)
	if b.Len() == 0 {
		// Fallback: trim quote characters off the raw source. Handles weird
		// grammar shapes (e.g. older tree-sitter-python versions) without
		// silently dropping the prompt.
		raw := nodeText(n, src)
		return trimQuotes(raw)
	}
	return b.String()
}

func trimQuotes(s string) string {
	for _, q := range []string{`"""`, `'''`, `"`, `'`} {
		if strings.HasPrefix(s, q) && strings.HasSuffix(s, q) && len(s) >= 2*len(q) {
			return s[len(q) : len(s)-len(q)]
		}
	}
	return s
}

func nodeText(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return n.Content(src)
}

// walk does a depth-first pre-order traversal, invoking fn at every node.
func walk(n *sitter.Node, fn func(*sitter.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for i := uint32(0); i < n.ChildCount(); i++ {
		walk(n.Child(int(i)), fn)
	}
}
