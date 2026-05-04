package index

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/kaeawc/evaldiff/internal/parse"
)

// Tool describes one statically-discovered tool definition: a top-level
// function decorated with @tool (or @claude.tool, etc.). Every field that
// could be derived from either the decorator or the function signature
// resolves the decorator first; the signature is the fallback.
type Tool struct {
	File        string      `json:"file"`
	Line        int         `json:"line"`        // 1-based line of the decorated definition
	Name        string      `json:"name"`        // explicit @tool(name=...) kwarg, else the function name
	Description Value       `json:"description"` // @tool(description=...), else the function docstring, else missing
	Params      []ToolParam `json:"params,omitempty"`
}

// ToolParam is one parameter of a tool function. Annotation captures the
// raw type-annotation source text (Value{Kind: ValueDynamic}) so we can
// diff annotation changes without trying to evaluate forward refs.
// Annotation.IsMissing means the parameter had no annotation at all.
type ToolParam struct {
	Name       string `json:"name"`
	Annotation Value  `json:"annotation"`
	HasDefault bool   `json:"has_default,omitempty"`
}

// ExtractTools finds every top-level @tool-decorated function in src and
// returns one Tool per match. Methods (functions inside a class) are
// intentionally skipped — Claude Agent SDK / OpenAI Agents SDK both prefer
// module-level tool functions, and including methods would require
// teaching the indexer about `self`.
func ExtractTools(ctx context.Context, file string, src []byte) ([]Tool, error) {
	r, err := parse.Parse(ctx, src)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var out []Tool
	walk(r.Root(), func(n *sitter.Node) {
		if n.Type() != "decorated_definition" {
			return
		}
		if !isTopLevel(n) {
			return
		}
		def := n.ChildByFieldName("definition")
		if def == nil || def.Type() != "function_definition" {
			return
		}
		dec := findToolDecorator(n, src)
		if dec == nil {
			return
		}
		out = append(out, buildTool(file, n, def, dec, src))
	})
	return out, nil
}

// toolDecorator describes a matched @tool decorator. CallNode is non-nil
// when the decorator was called (e.g. @tool(name="x")); for a bare @tool
// it stays nil and Kwargs is empty.
type toolDecorator struct {
	CallNode *sitter.Node
	Kwargs   map[string]Value
}

// findToolDecorator returns the first decorator on n whose target is
// `tool` or `*.tool`. Both the bare and called forms are accepted.
func findToolDecorator(n *sitter.Node, src []byte) *toolDecorator {
	for i := uint32(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(int(i))
		if c.Type() != "decorator" {
			continue
		}
		expr := decoratorExpr(c)
		if expr == nil {
			continue
		}
		callee, callNode := decoratorCallee(expr)
		if !isToolCallee(nodeText(callee, src)) {
			continue
		}
		dec := &toolDecorator{CallNode: callNode, Kwargs: map[string]Value{}}
		if callNode != nil {
			if argList := callNode.ChildByFieldName("arguments"); argList != nil {
				forEachKwarg(argList, src, func(name string, v Value) {
					dec.Kwargs[name] = v
				})
			}
		}
		return dec
	}
	return nil
}

// decoratorExpr returns the expression inside a decorator node, skipping
// the leading "@" anonymous token. Older grammars expose this as the only
// named child; we just take the first named child for forward compatibility.
func decoratorExpr(dec *sitter.Node) *sitter.Node {
	if dec.NamedChildCount() == 0 {
		return nil
	}
	return dec.NamedChild(0)
}

// decoratorCallee returns the callee expression of a decorator, plus the
// call node when the decorator was invoked. For `@tool` the expression
// itself is the callee and call is nil; for `@tool(...)` we descend into
// the call's `function` field.
func decoratorCallee(expr *sitter.Node) (callee, call *sitter.Node) {
	if expr.Type() == "call" {
		return expr.ChildByFieldName("function"), expr
	}
	return expr, nil
}

// toolDecoratorNames are the unqualified decorator names recognized as
// declaring a tool. Both bare and dotted-suffix forms match (e.g. `tool`,
// `mcp.tool`, `function_tool`, `agents.function_tool`). Adding a new
// framework here unlocks its tools without other changes.
var toolDecoratorNames = []string{"tool", "function_tool"}

func isToolCallee(name string) bool {
	for _, want := range toolDecoratorNames {
		if name == want {
			return true
		}
		if strings.HasSuffix(name, "."+want) {
			return true
		}
	}
	return false
}

// nameKwargs and descriptionKwargs list the decorator kwargs that map to
// Tool.Name / Tool.Description. The first literal hit wins, so when a
// framework supports both `name` and `name_override` the canonical one
// can be listed first.
var (
	nameKwargs        = []string{"name", "name_override"}
	descriptionKwargs = []string{"description", "description_override"}
)

func buildTool(file string, decoratedDef, def *sitter.Node, dec *toolDecorator, src []byte) Tool {
	name := nodeText(def.ChildByFieldName("name"), src)
	if v, ok := firstLiteralKwarg(dec.Kwargs, nameKwargs); ok {
		name = v.Str
	}
	desc, ok := firstLiteralKwarg(dec.Kwargs, descriptionKwargs)
	if !ok {
		desc = docstring(def, src)
	}
	return Tool{
		File:        file,
		Line:        int(decoratedDef.StartPoint().Row) + 1,
		Name:        name,
		Description: desc,
		Params:      extractParams(def, src),
	}
}

// firstLiteralKwarg returns the first literal Value found by walking
// keys in order. ok is false when none of the requested kwargs is
// present as a literal.
func firstLiteralKwarg(kw map[string]Value, keys []string) (Value, bool) {
	for _, k := range keys {
		if v, ok := kw[k]; ok && v.IsLiteral() {
			return v, true
		}
	}
	return Value{}, false
}

// docstring returns the function's docstring as a ValueLiteral, or
// ValueMissing if the body's first statement is not a bare string.
func docstring(def *sitter.Node, src []byte) Value {
	body := def.ChildByFieldName("body")
	if body == nil || body.NamedChildCount() == 0 {
		return Value{}
	}
	first := body.NamedChild(0)
	if first.Type() != "expression_statement" || first.NamedChildCount() == 0 {
		return Value{}
	}
	str := first.NamedChild(0)
	if str.Type() != "string" {
		return Value{}
	}
	return Value{Kind: ValueLiteral, Str: stringContent(str, src), Source: nodeText(str, src)}
}

// extractParams returns one ToolParam per declared parameter, in source
// order. Annotation is ValueDynamic with the annotation source text, or
// ValueMissing when the parameter has no annotation. *args / **kwargs and
// positional-only / keyword-only markers are skipped.
func extractParams(def *sitter.Node, src []byte) []ToolParam {
	params := def.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	var out []ToolParam
	for i := uint32(0); i < params.NamedChildCount(); i++ {
		p := params.NamedChild(int(i))
		switch p.Type() {
		case "identifier":
			out = append(out, ToolParam{Name: nodeText(p, src)})
		case "typed_parameter":
			out = append(out, typedParam(p, src, false))
		case "default_parameter":
			out = append(out, defaultParam(p, src))
		case "typed_default_parameter":
			out = append(out, typedParam(p, src, true))
		}
	}
	return out
}

func typedParam(n *sitter.Node, src []byte, hasDefault bool) ToolParam {
	tp := ToolParam{HasDefault: hasDefault}
	if name := firstChildOfType(n, "identifier"); name != nil {
		tp.Name = nodeText(name, src)
	}
	if t := n.ChildByFieldName("type"); t != nil {
		tp.Annotation = Value{Kind: ValueDynamic, Source: nodeText(t, src)}
	}
	return tp
}

func defaultParam(n *sitter.Node, src []byte) ToolParam {
	tp := ToolParam{HasDefault: true}
	if name := n.ChildByFieldName("name"); name != nil {
		tp.Name = nodeText(name, src)
	}
	return tp
}

func firstChildOfType(n *sitter.Node, kind string) *sitter.Node {
	for i := uint32(0); i < n.NamedChildCount(); i++ {
		c := n.NamedChild(int(i))
		if c.Type() == kind {
			return c
		}
	}
	return nil
}

// isTopLevel returns true if n's parent is the module root (i.e. n is a
// module-level statement, not nested in a class or another function).
func isTopLevel(n *sitter.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return true
	}
	return parent.Type() == "module"
}
