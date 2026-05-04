package diff

import (
	"sort"

	"github.com/kaeawc/evaldiff/internal/index"
)

// ParamsDiff describes how a tool's parameter list changed between two
// versions. Identity is by parameter name, so a renamed parameter
// surfaces as one removal and one addition. Reorder-only edits produce
// an empty ParamsDiff (Added, Removed, Modified all empty); the parent
// ToolMod still flags Fields=[params] because positional ordering is a
// real Python-signature change even when the JSON schema is unaffected.
type ParamsDiff struct {
	Added    []index.ToolParam `json:"added,omitempty"`
	Removed  []index.ToolParam `json:"removed,omitempty"`
	Modified []ParamMod        `json:"modified,omitempty"`
}

// IsEmpty reports whether the ParamsDiff describes no per-name change
// (i.e. a reorder-only edit).
func (p *ParamsDiff) IsEmpty() bool {
	return len(p.Added) == 0 && len(p.Removed) == 0 && len(p.Modified) == 0
}

// ParamMod describes one parameter present in both before and after with
// the same name but at least one differing field. Fields lists the
// names of changed fields ("annotation", "has_default").
type ParamMod struct {
	Before index.ToolParam `json:"before"`
	After  index.ToolParam `json:"after"`
	Fields []string        `json:"fields"`
}

// paramsStructuralDiff returns a ParamsDiff describing per-name
// add/remove/modify events between before and after. The returned
// pointer is never nil; callers can rely on iteration over its slices.
func paramsStructuralDiff(before, after []index.ToolParam) *ParamsDiff {
	beforeMap := indexParams(before)
	afterMap := indexParams(after)
	out := &ParamsDiff{}
	for _, name := range sortedNames(beforeMap, afterMap) {
		b, hasB := beforeMap[name]
		a, hasA := afterMap[name]
		switch {
		case hasB && !hasA:
			out.Removed = append(out.Removed, b)
		case !hasB && hasA:
			out.Added = append(out.Added, a)
		case hasB && hasA:
			if fields := paramFieldDiff(b, a); len(fields) > 0 {
				out.Modified = append(out.Modified, ParamMod{Before: b, After: a, Fields: fields})
			}
		}
	}
	return out
}

func indexParams(params []index.ToolParam) map[string]index.ToolParam {
	out := make(map[string]index.ToolParam, len(params))
	for _, p := range params {
		out[p.Name] = p
	}
	return out
}

func sortedNames(a, b map[string]index.ToolParam) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func paramFieldDiff(b, a index.ToolParam) []string {
	var out []string
	if !valueEqual(b.Annotation, a.Annotation) {
		out = append(out, "annotation")
	}
	if b.HasDefault != a.HasDefault {
		out = append(out, "has_default")
	}
	return out
}
