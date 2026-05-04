package coverage

import (
	"context"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/kaeawc/evaldiff/internal/parse"
)

// ExtractTests finds every pytest-style test function in src and returns
// one TestEntry per match. Two shapes are recognized:
//
//   - top-level `def test_*(...)` (with or without decorators).
//   - methods named `test_*` inside a class whose name starts with "Test".
//
// File is stamped onto each entry from the file argument; src is the
// Python source bytes.
func ExtractTests(ctx context.Context, file string, src []byte) ([]TestEntry, error) {
	r, err := parse.Parse(ctx, src)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var out []TestEntry
	root := r.Root()
	for i := uint32(0); i < root.NamedChildCount(); i++ {
		stmt := root.NamedChild(int(i))
		switch stmt.Type() {
		case "function_definition":
			if entry, ok := topLevelTest(stmt, stmt, file, src); ok {
				out = append(out, entry)
			}
		case "decorated_definition":
			def := stmt.ChildByFieldName("definition")
			if def != nil && def.Type() == "function_definition" {
				if entry, ok := topLevelTest(stmt, def, file, src); ok {
					out = append(out, entry)
				}
			}
		case "class_definition":
			out = append(out, classTests(stmt, file, src)...)
		}
	}
	return out, nil
}

// topLevelTest returns a TestEntry when def is a `test_*` function. The
// outer node (the decorated_definition or the function_definition itself)
// is used for the line number so a decorator above the function sets the
// reported line.
func topLevelTest(outer, def *sitter.Node, file string, src []byte) (TestEntry, bool) {
	name := nodeText(def.ChildByFieldName("name"), src)
	if !isTestName(name) {
		return TestEntry{}, false
	}
	return TestEntry{
		File: file,
		Line: int(outer.StartPoint().Row) + 1,
		Name: name,
	}, true
}

// classTests returns one TestEntry per `test_*` method on cls when cls's
// name starts with "Test" and it has no `__init__` (pytest's collection
// rule). Inherited methods are not resolved.
func classTests(cls *sitter.Node, file string, src []byte) []TestEntry {
	className := nodeText(cls.ChildByFieldName("name"), src)
	if !isTestClassName(className) {
		return nil
	}
	body := cls.ChildByFieldName("body")
	if body == nil {
		return nil
	}
	if classHasInit(body, src) {
		return nil
	}
	var out []TestEntry
	for i := uint32(0); i < body.NamedChildCount(); i++ {
		stmt := body.NamedChild(int(i))
		var def, outer *sitter.Node
		switch stmt.Type() {
		case "function_definition":
			def, outer = stmt, stmt
		case "decorated_definition":
			d := stmt.ChildByFieldName("definition")
			if d == nil || d.Type() != "function_definition" {
				continue
			}
			def, outer = d, stmt
		default:
			continue
		}
		methodName := nodeText(def.ChildByFieldName("name"), src)
		if !isTestName(methodName) {
			continue
		}
		out = append(out, TestEntry{
			File:  file,
			Line:  int(outer.StartPoint().Row) + 1,
			Name:  methodName,
			Class: className,
		})
	}
	return out
}

func classHasInit(body *sitter.Node, src []byte) bool {
	for i := uint32(0); i < body.NamedChildCount(); i++ {
		stmt := body.NamedChild(int(i))
		if stmt.Type() != "function_definition" {
			continue
		}
		if nodeText(stmt.ChildByFieldName("name"), src) == "__init__" {
			return true
		}
	}
	return false
}

func isTestName(name string) bool      { return strings.HasPrefix(name, "test_") || name == "test" }
func isTestClassName(name string) bool { return strings.HasPrefix(name, "Test") }

func nodeText(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return n.Content(src)
}
