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
	Name        Value  `json:"name"`        // literal value of the `name` kwarg, used as stable identity when present
	Model       Value  `json:"model"`
	System      Value  `json:"system"`
	Tools       Value  `json:"tools"`
	// ToolNames is the parsed identifier list when Tools is a Python
	// list literal of bare identifiers (e.g. tools=[search, browse]).
	// Empty when Tools is missing, dynamic without a recognizable shape,
	// or contains non-identifier expressions like calls or attributes.
	// Downstream coverage uses it to build agent → tool edges without
	// re-walking the source.
	ToolNames []string `json:"tool_names,omitempty"`
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
			forEachKwargNode(argList, src, func(kw string, valueNode *sitter.Node, value Value) {
				switch kw {
				case "name":
					a.Name = value
				case "model":
					a.Model = value
				case "system", "system_prompt", "instructions":
					if a.System.IsMissing() {
						a.System = value
					}
				case "tools":
					a.Tools = value
					a.ToolNames = extractIdentifierList(valueNode, src)
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
	forEachKwargNode(argList, src, func(name string, _ *sitter.Node, v Value) {
		fn(name, v)
	})
}

// forEachKwargNode is like forEachKwarg but also passes the raw value
// AST node so callers can do shape-specific extraction (e.g. parsing a
// list literal into an identifier slice) that a flat Value can't carry.
func forEachKwargNode(argList *sitter.Node, src []byte, fn func(name string, valueNode *sitter.Node, v Value)) {
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
		fn(name, valueNode, valueFromNode(valueNode, src))
	}
}

// extractIdentifierList returns the bare identifier names inside a
// Python list literal. Returns nil when the value isn't a list, or
// when any element isn't a plain identifier — partial extraction
// would mislead consumers about which tools an agent uses.
func extractIdentifierList(n *sitter.Node, src []byte) []string {
	if n == nil {
		return nil
	}
	inner := unwrapParens(n)
	if inner == nil || inner.Type() != "list" {
		return nil
	}
	var out []string
	for i := uint32(0); i < inner.NamedChildCount(); i++ {
		c := inner.NamedChild(int(i))
		if c.Type() != "identifier" {
			return nil
		}
		out = append(out, nodeText(c, src))
	}
	return out
}

// forEachPositional invokes fn for every non-keyword argument under
// argList, in source order, with its resolved Value. Used by callers
// (like the @tool extractor) where positional argument order carries
// meaning per the framework's API.
func forEachPositional(argList *sitter.Node, src []byte, fn func(v Value)) {
	for i := uint32(0); i < argList.NamedChildCount(); i++ {
		arg := argList.NamedChild(int(i))
		if arg.Type() == "keyword_argument" {
			continue
		}
		fn(valueFromNode(arg, src))
	}
}

// valueFromNode resolves a tree-sitter value node into a Value.
// Recognized as literals:
//   - "string"               – plain quoted string
//   - "concatenated_string"  – Python's implicit-concatenation form,
//     e.g. ("hello " "world") or two string
//     literals separated only by whitespace
//   - "parenthesized_expression" wrapping either of the above
//
// Everything else is ValueDynamic with the raw source text preserved.
// Source always reflects the original (parens included) so diff
// renderers see what the developer wrote, while Str is the collapsed
// payload suitable for token-level prompt comparison.
func valueFromNode(n *sitter.Node, src []byte) Value {
	source := nodeText(n, src)
	inner := unwrapParens(n)
	if inner != nil && (inner.Type() == "string" || inner.Type() == "concatenated_string") {
		return Value{Kind: ValueLiteral, Str: stringContent(inner, src), Source: source}
	}
	return Value{Kind: ValueDynamic, Source: source}
}

// unwrapParens descends through parenthesized_expression wrappers that
// contain a single inner expression, returning the innermost node. A
// parenthesized expression with multiple children (rare; tuples shape
// differently) is returned unchanged.
func unwrapParens(n *sitter.Node) *sitter.Node {
	for n != nil && n.Type() == "parenthesized_expression" && n.NamedChildCount() == 1 {
		n = n.NamedChild(0)
	}
	return n
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
