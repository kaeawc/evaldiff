package coverage

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/kaeawc/evaldiff/internal/index"
	"github.com/kaeawc/evaldiff/internal/vfs"
)

// AttachTouches mutates cov in place: for every test, populates Touches
// with the BehaviorRefs from every imported file the test actually
// references *some* name from.
//
// Per-test refinement is "imports the test uses, file-coarse on the
// other side" — referencing a helper name from a file pulls in every
// behavior defined in that file. This catches the dominant pattern of
// tests calling a helper that internally uses agents/tools (the helper
// name is what the test references; the agents are co-located with the
// helper in the same module).
//
// Imports that don't resolve to a file in idx (third-party deps,
// namespace packages, relative imports we don't follow yet) are silently
// skipped: the user sees emptier-than-perfect Touches but the tool keeps
// working.
func AttachTouches(ctx context.Context, fs vfs.FS, cov *Coverage, idx *index.Index) error {
	if cov == nil || idx == nil {
		return nil
	}
	refsByFile := buildRefsByFile(idx)
	resolver := newImportResolver(idx)
	byFile := groupTestsByFile(cov)
	for file, testIxes := range byFile {
		bindings, err := buildFileBindings(ctx, fs, cov.Root, file, resolver, refsByFile)
		if err != nil {
			return err
		}
		for _, i := range testIxes {
			cov.Tests[i].Touches = touchesForTest(cov.Tests[i].Identifiers, bindings)
		}
	}
	return nil
}

// fileBinding pairs the local-names a single import statement bound in
// the test file (e.g. ["search", "browse"] for `from app.tools import
// search, browse`) with the behaviors that import grants access to (all
// behaviors defined in the resolved target file).
type fileBinding struct {
	names     []string
	behaviors []BehaviorRef
}

// touchesForTest returns the union of behaviors from every binding
// whose `names` list intersects with `identifiers` — i.e. for every
// import the test references at least one name from, include all
// behaviors in that import's target file. Result is deduped + sorted.
func touchesForTest(identifiers []string, bindings []fileBinding) []BehaviorRef {
	if len(identifiers) == 0 || len(bindings) == 0 {
		return nil
	}
	idSet := make(map[string]struct{}, len(identifiers))
	for _, id := range identifiers {
		idSet[id] = struct{}{}
	}
	var touches []BehaviorRef
	for _, b := range bindings {
		if !anyInSet(b.names, idSet) {
			continue
		}
		touches = append(touches, b.behaviors...)
	}
	return dedupeRefs(touches)
}

func anyInSet(names []string, set map[string]struct{}) bool {
	for _, n := range names {
		if _, ok := set[n]; ok {
			return true
		}
	}
	return false
}

// buildRefsByFile flattens an index into "given a file path, what
// behaviors live in it." Agent BehaviorRefs use the literal `name`
// kwarg when present (the SDK convention) and fall back to
// constructor#ordinal so unnamed agents still have a stable identity
// matching the diff package's heuristic. Tool refs use the tool's own
// name.
func buildRefsByFile(idx *index.Index) map[string][]BehaviorRef {
	out := map[string][]BehaviorRef{}
	for _, fe := range idx.Files {
		for i, a := range fe.Agents {
			out[fe.File] = append(out[fe.File], BehaviorRef{
				Kind: "agent",
				File: fe.File,
				Name: agentRefName(a, i),
			})
		}
		for _, t := range fe.Tools {
			out[fe.File] = append(out[fe.File], BehaviorRef{
				Kind: "tool",
				File: fe.File,
				Name: t.Name,
			})
		}
	}
	return out
}

// AgentRefName returns the BehaviorRef.Name evaldiff uses for an agent:
// its literal `name` kwarg when present (the SDK convention), else
// constructor#ordinal. Exported so other packages computing the same
// identity (e.g. internal/rank) stay in sync without re-deriving.
func AgentRefName(a index.Agent, ordinal int) string {
	if a.Name.IsLiteral() {
		return a.Name.Str
	}
	return a.Constructor + "#" + strconv.Itoa(ordinal)
}

func agentRefName(a index.Agent, ordinal int) string { return AgentRefName(a, ordinal) }

func groupTestsByFile(cov *Coverage) map[string][]int {
	out := map[string][]int{}
	for i, t := range cov.Tests {
		out[t.File] = append(out[t.File], i)
	}
	return out
}

// buildFileBindings parses one test file's imports and returns one
// fileBinding per import that resolves to a file with behaviors.
// The local-names list captures every name the import statement bound
// in the test file's namespace; the behaviors list is the full
// behavior set from the resolved target file.
//
// Resolution rules:
//   - `from X import a, b as c` → names=[a, c], behaviors = all in X
//     (and tries each item as a submodule X.a, X.b — if it resolves,
//     a separate binding is emitted with names=[a or c] and behaviors
//     = all in the submodule).
//   - `from X import *` → no local-names captured (we can't tell what
//     was glob-imported); skipped today.
//   - `import X` → names=[lastDotted(X)], behaviors = all in X.
//   - `import X as Y` → names=[Y], behaviors = all in X.
func buildFileBindings(
	ctx context.Context,
	fs vfs.FS,
	root, relPath string,
	resolver *importResolver,
	refsByFile map[string][]BehaviorRef,
) ([]fileBinding, error) {
	src, err := fs.Read(filepath.Join(root, relPath))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", relPath, err)
	}
	imports, err := ExtractImports(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("imports %s: %w", relPath, err)
	}
	var out []fileBinding
	for _, imp := range imports {
		out = appendBindings(out, imp, resolver, refsByFile)
	}
	return out, nil
}

func appendBindings(out []fileBinding, imp Import, resolver *importResolver, refsByFile map[string][]BehaviorRef) []fileBinding {
	if len(imp.Items) == 0 {
		return appendModuleImport(out, imp, resolver, refsByFile)
	}
	return appendFromImport(out, imp, resolver, refsByFile)
}

// appendModuleImport handles `import X` and `import X as Y` — the
// locally-bound name is the alias (or the last dotted component of X)
// and grants access to every behavior in module X.
func appendModuleImport(out []fileBinding, imp Import, resolver *importResolver, refsByFile map[string][]BehaviorRef) []fileBinding {
	target := resolver.resolve(imp.Module)
	if target == "" {
		return out
	}
	alias := imp.ModuleAlias
	if alias == "" {
		alias = lastDotted(imp.Module)
	}
	if alias == "" {
		return out
	}
	if behaviors := refsByFile[target]; len(behaviors) > 0 {
		return append(out, fileBinding{names: []string{alias}, behaviors: behaviors})
	}
	return out
}

// appendFromImport handles `from X import a, b as c`. Items that
// resolve as submodules (X.a) get their own binding; the rest are
// grouped under one binding pointing at module X (every shared local-
// name reaches the same target file).
func appendFromImport(out []fileBinding, imp Import, resolver *importResolver, refsByFile map[string][]BehaviorRef) []fileBinding {
	parentNames := []string{}
	for _, item := range imp.Items {
		local := localItemName(item)
		if local == "" {
			continue
		}
		if added, ok := bindAsSubmodule(imp.Module, item.Name, local, resolver, refsByFile); ok {
			out = append(out, added)
			continue
		}
		parentNames = append(parentNames, local)
	}
	if len(parentNames) == 0 {
		return out
	}
	target := resolver.resolve(imp.Module)
	if target == "" {
		return out
	}
	if behaviors := refsByFile[target]; len(behaviors) > 0 {
		out = append(out, fileBinding{names: parentNames, behaviors: behaviors})
	}
	return out
}

// bindAsSubmodule returns a binding for `from X import item` when
// X.item resolves as its own module file (and that file has behaviors).
// Returns ok=false otherwise so the caller falls through to the
// parent-module binding.
func bindAsSubmodule(module, itemName, local string, resolver *importResolver, refsByFile map[string][]BehaviorRef) (fileBinding, bool) {
	subTarget := resolver.resolve(module + "." + itemName)
	if subTarget == "" {
		return fileBinding{}, false
	}
	behaviors := refsByFile[subTarget]
	if len(behaviors) == 0 {
		return fileBinding{}, false
	}
	return fileBinding{names: []string{local}, behaviors: behaviors}, true
}

func localItemName(item ImportItem) string {
	if item.Alias != "" {
		return item.Alias
	}
	return item.Name
}

func lastDotted(module string) string {
	if i := strings.LastIndex(module, "."); i >= 0 {
		return module[i+1:]
	}
	return module
}

// dedupeRefs removes duplicate BehaviorRefs and sorts the result by
// (Kind, File, Name) so two AttachTouches runs on the same input
// produce byte-identical Coverage JSON.
func dedupeRefs(in []BehaviorRef) []BehaviorRef {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[BehaviorRef]struct{}, len(in))
	out := make([]BehaviorRef, 0, len(in))
	for _, r := range in {
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// importResolver maps a Python module name (dotted) to the matching file
// path in the behavior index, or "" if no match. Supports package files
// (foo/__init__.py) and module files (foo.py), plus PEP 517 / "src layout"
// projects where the package lives one directory deeper (e.g. src/foo/).
// Does not follow relative imports today.
type importResolver struct {
	files    map[string]struct{}
	srcRoots []string // path prefixes with trailing slash, e.g. "src/"
}

// knownSrcRoots are directories conventionally used as PEP 517 source
// roots (the dir contains the package; the dir itself is not part of the
// dotted module name). When the indexed tree contains files under one of
// these prefixes, the resolver also tries module names against the
// stripped path.
var knownSrcRoots = []string{"src/"}

func newImportResolver(idx *index.Index) *importResolver {
	r := &importResolver{files: make(map[string]struct{}, len(idx.Files))}
	seen := map[string]struct{}{}
	for _, fe := range idx.Files {
		r.files[fe.File] = struct{}{}
		for _, root := range knownSrcRoots {
			if strings.HasPrefix(fe.File, root) {
				if _, ok := seen[root]; !ok {
					seen[root] = struct{}{}
					r.srcRoots = append(r.srcRoots, root)
				}
			}
		}
	}
	return r
}

// resolve returns the path of the indexed file that "module" points at,
// or "" when no candidate matches. Candidates are tried in order:
//  1. <module>.py at root
//  2. <module>/__init__.py at root
//  3. for each detected src root, the same two under that prefix
//
// Root-level wins, so a real top-level module shadows one of the same
// name that happens to also exist under src/.
func (r *importResolver) resolve(module string) string {
	if module == "" || strings.HasPrefix(module, ".") {
		return ""
	}
	rel := strings.ReplaceAll(module, ".", "/")
	if hit := r.firstMatch(rel, ""); hit != "" {
		return hit
	}
	for _, root := range r.srcRoots {
		if hit := r.firstMatch(rel, root); hit != "" {
			return hit
		}
	}
	return ""
}

func (r *importResolver) firstMatch(rel, prefix string) string {
	if cand := prefix + rel + ".py"; r.has(cand) {
		return cand
	}
	if cand := prefix + rel + "/__init__.py"; r.has(cand) {
		return cand
	}
	return ""
}

func (r *importResolver) has(p string) bool {
	_, ok := r.files[p]
	return ok
}
