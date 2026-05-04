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
// with one BehaviorRef per Agent or Tool whose owning file is imported
// by the test's enclosing module. Mapping is file-coarse — every test in
// a module gets the same Touches union, since static analysis can't tell
// which test in a file actually exercises which import. Per-test
// refinement is a future slice.
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
		touches, err := touchesForFile(ctx, fs, cov.Root, file, resolver, refsByFile)
		if err != nil {
			return err
		}
		for _, i := range testIxes {
			cov.Tests[i].Touches = touches
		}
	}
	return nil
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

func touchesForFile(ctx context.Context, fs vfs.FS, root, relPath string, resolver *importResolver, refsByFile map[string][]BehaviorRef) ([]BehaviorRef, error) {
	src, err := fs.Read(filepath.Join(root, relPath))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", relPath, err)
	}
	imports, err := ExtractImports(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("imports %s: %w", relPath, err)
	}
	var touches []BehaviorRef
	for _, imp := range imports {
		if target := resolver.resolve(imp.Module); target != "" {
			touches = append(touches, refsByFile[target]...)
		}
		// For "from X import Y" each Y may be either a name defined inside
		// module X *or* a submodule X.Y; Python's import system tries both.
		// We do too, so `from app import tools` resolves to app/tools.py or
		// app/tools/__init__.py even when "app" itself isn't a module file.
		for _, item := range imp.Items {
			if item.Name == "" {
				continue
			}
			sub := imp.Module + "." + item.Name
			if target := resolver.resolve(sub); target != "" {
				touches = append(touches, refsByFile[target]...)
			}
		}
	}
	return dedupeRefs(touches), nil
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
