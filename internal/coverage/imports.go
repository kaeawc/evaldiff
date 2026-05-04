package coverage

import (
	"context"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/kaeawc/evaldiff/internal/parse"
)

// Import is a single import statement parsed out of a Python source file.
//
//   - "import x"            → Import{Module: "x"}
//   - "import x as y"       → Import{Module: "x", ModuleAlias: "y"}
//   - "from x import a"     → Import{Module: "x", Items: [{Name:"a"}]}
//   - "from x import a as b"→ Import{Module: "x", Items: [{Name:"a", Alias:"b"}]}
//   - "from x import a, b"  → Import{Module: "x", Items: [{Name:"a"}, {Name:"b"}]}
//
// Relative imports ("from .foo import x") set Module to the dotted name
// preserving leading dots so AttachTouches can either resolve them in
// future or skip them today.
type Import struct {
	Module      string
	ModuleAlias string
	Items       []ImportItem
}

// ImportItem is one entry in a "from X import a, b as c" list.
type ImportItem struct {
	Name  string
	Alias string
}

// ExtractImports walks src and returns one Import per top-level import
// statement. Imports nested inside functions, conditionals, or try/except
// blocks are intentionally ignored — the import-graph heuristic keys on
// module-level imports because that's where pytest tests pull their
// fixtures and helpers from.
func ExtractImports(ctx context.Context, src []byte) ([]Import, error) {
	r, err := parse.Parse(ctx, src)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var out []Import
	root := r.Root()
	for i := uint32(0); i < root.NamedChildCount(); i++ {
		stmt := root.NamedChild(int(i))
		switch stmt.Type() {
		case "import_statement":
			out = append(out, parseImportStatement(stmt, src)...)
		case "import_from_statement":
			if imp, ok := parseImportFrom(stmt, src); ok {
				out = append(out, imp)
			}
		}
	}
	return out, nil
}

// parseImportStatement handles `import a, b.c, d as alias`.
//
// tree-sitter shape: import_statement → name child(ren). Each name is
// either a `dotted_name` (plain) or an `aliased_import` (with `name` and
// `alias` field children).
func parseImportStatement(stmt *sitter.Node, src []byte) []Import {
	var out []Import
	for i := uint32(0); i < stmt.NamedChildCount(); i++ {
		c := stmt.NamedChild(int(i))
		switch c.Type() {
		case "dotted_name":
			out = append(out, Import{Module: nodeText(c, src)})
		case "aliased_import":
			module := nodeText(c.ChildByFieldName("name"), src)
			alias := nodeText(c.ChildByFieldName("alias"), src)
			out = append(out, Import{Module: module, ModuleAlias: alias})
		}
	}
	return out
}

// parseImportFrom handles `from X import a, b as c`. Wildcard imports
// (`from X import *`) yield Import{Module: "X"} with empty Items so the
// downstream resolver still gets a chance to map the module to a file.
func parseImportFrom(stmt *sitter.Node, src []byte) (Import, bool) {
	moduleNode := stmt.ChildByFieldName("module_name")
	if moduleNode == nil {
		// Relative imports use `relative_import` instead. tree-sitter exposes
		// it as a sibling without the module_name field; fall back to the
		// first non-import-keyword child.
		moduleNode = firstNonKeyword(stmt)
	}
	module := nodeText(moduleNode, src)
	if module == "" {
		return Import{}, false
	}
	imp := Import{Module: module}
	for i := uint32(0); i < stmt.NamedChildCount(); i++ {
		c := stmt.NamedChild(int(i))
		if c == moduleNode {
			continue
		}
		switch c.Type() {
		case "dotted_name":
			imp.Items = append(imp.Items, ImportItem{Name: nodeText(c, src)})
		case "aliased_import":
			imp.Items = append(imp.Items, ImportItem{
				Name:  nodeText(c.ChildByFieldName("name"), src),
				Alias: nodeText(c.ChildByFieldName("alias"), src),
			})
		}
	}
	return imp, true
}

// firstNonKeyword returns the first named child that isn't the literal
// "from"/"import" tokens. Used to find the module node for relative
// imports where tree-sitter doesn't expose the module_name field.
func firstNonKeyword(stmt *sitter.Node) *sitter.Node {
	for i := uint32(0); i < stmt.NamedChildCount(); i++ {
		c := stmt.NamedChild(int(i))
		switch c.Type() {
		case "dotted_name", "relative_import":
			return c
		}
	}
	return nil
}
