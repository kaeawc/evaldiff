// Package diff produces a semantic diff over two behavior indexes:
// added/removed agents and tools, plus per-field modifications. Later
// slices add token-level prompt diffs and structural tool-schema diffs.
package diff

import (
	"sort"
	"strconv"

	"github.com/kaeawc/evaldiff/internal/index"
)

// Changeset describes the differences between two behavior indexes.
// Fields are populated even when empty so JSON consumers see consistent
// shape; nil slices marshal as null otherwise.
type Changeset struct {
	Agents AgentChanges `json:"agents"`
	Tools  ToolChanges  `json:"tools"`
}

// IsEmpty reports whether the changeset describes no changes at all.
func (c *Changeset) IsEmpty() bool {
	return len(c.Agents.Added) == 0 && len(c.Agents.Removed) == 0 && len(c.Agents.Modified) == 0 &&
		len(c.Tools.Added) == 0 && len(c.Tools.Removed) == 0 && len(c.Tools.Modified) == 0
}

type AgentChanges struct {
	Added    []index.Agent `json:"added,omitempty"`
	Removed  []index.Agent `json:"removed,omitempty"`
	Modified []AgentMod    `json:"modified,omitempty"`
}

type ToolChanges struct {
	Added    []index.Tool `json:"added,omitempty"`
	Removed  []index.Tool `json:"removed,omitempty"`
	Modified []ToolMod    `json:"modified,omitempty"`
}

// AgentMod describes one agent that exists in both indexes but with at
// least one differing extracted field. Fields lists the field names that
// differ (e.g. "model", "system") so PR-comment renderers can summarize.
type AgentMod struct {
	Before index.Agent `json:"before"`
	After  index.Agent `json:"after"`
	Fields []string    `json:"fields"`
}

// ToolMod is the tool counterpart of AgentMod.
type ToolMod struct {
	Before index.Tool `json:"before"`
	After  index.Tool `json:"after"`
	Fields []string   `json:"fields"`
}

// Diff returns the changeset describing how head differs from base. Agent
// identity is (file, ordinal-among-agents-in-that-file); a moved agent
// within the same file is treated as a modification, but a moved agent
// across files appears as a remove + add. Tool identity is (file, name).
//
// Both heuristics will flag false-positive removes when files are renamed
// or split. A later slice can add structural matching across files.
func Diff(base, head *index.Index) *Changeset {
	cs := &Changeset{}
	diffAgents(base, head, &cs.Agents)
	diffTools(base, head, &cs.Tools)
	return cs
}

func diffAgents(base, head *index.Index, out *AgentChanges) {
	beforeMap := indexAgents(base)
	afterMap := indexAgents(head)
	keys := unionKeys(beforeMap, afterMap)
	for _, k := range keys {
		b, hasB := beforeMap[k]
		a, hasA := afterMap[k]
		switch {
		case hasB && !hasA:
			out.Removed = append(out.Removed, b)
		case !hasB && hasA:
			out.Added = append(out.Added, a)
		case hasB && hasA:
			if fields := agentFieldDiff(b, a); len(fields) > 0 {
				out.Modified = append(out.Modified, AgentMod{Before: b, After: a, Fields: fields})
			}
		}
	}
}

func diffTools(base, head *index.Index, out *ToolChanges) {
	beforeMap := indexTools(base)
	afterMap := indexTools(head)
	keys := unionKeys(beforeMap, afterMap)
	for _, k := range keys {
		b, hasB := beforeMap[k]
		a, hasA := afterMap[k]
		switch {
		case hasB && !hasA:
			out.Removed = append(out.Removed, b)
		case !hasB && hasA:
			out.Added = append(out.Added, a)
		case hasB && hasA:
			if fields := toolFieldDiff(b, a); len(fields) > 0 {
				out.Modified = append(out.Modified, ToolMod{Before: b, After: a, Fields: fields})
			}
		}
	}
}

// indexAgents groups all agents by (file, ordinal-within-file). Iterating
// the input file list gives ordinals in source order so they're stable
// across builds of the same tree.
func indexAgents(idx *index.Index) map[string]index.Agent {
	out := map[string]index.Agent{}
	if idx == nil {
		return out
	}
	for _, f := range idx.Files {
		for i, a := range f.Agents {
			out[agentKey(f.File, i)] = a
		}
	}
	return out
}

func indexTools(idx *index.Index) map[string]index.Tool {
	out := map[string]index.Tool{}
	if idx == nil {
		return out
	}
	for _, f := range idx.Files {
		for _, t := range f.Tools {
			out[toolKey(f.File, t.Name)] = t
		}
	}
	return out
}

func agentKey(file string, ordinal int) string {
	return file + "#" + strconv.Itoa(ordinal)
}

func toolKey(file, name string) string {
	return file + "::" + name
}

// unionKeys returns the union of two map's keys in lexically-sorted order
// for stable Changeset output.
func unionKeys[V any](a, b map[string]V) []string {
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

// agentFieldDiff returns the names of Agent fields that differ between b
// and a. File and Line are excluded — moving an agent within a file is
// not a behavior change. Constructor is included because Agent vs
// claude.Agent has the same name from the model's perspective but a
// renamed import path is a real, reviewable change.
func agentFieldDiff(b, a index.Agent) []string {
	var out []string
	if b.Constructor != a.Constructor {
		out = append(out, "constructor")
	}
	if !valueEqual(b.Model, a.Model) {
		out = append(out, "model")
	}
	if !valueEqual(b.System, a.System) {
		out = append(out, "system")
	}
	if !valueEqual(b.Tools, a.Tools) {
		out = append(out, "tools")
	}
	return out
}

func toolFieldDiff(b, a index.Tool) []string {
	var out []string
	if b.Name != a.Name {
		out = append(out, "name")
	}
	if !valueEqual(b.Description, a.Description) {
		out = append(out, "description")
	}
	if !paramsEqual(b.Params, a.Params) {
		out = append(out, "params")
	}
	return out
}

func valueEqual(a, b index.Value) bool {
	return a.Kind == b.Kind && a.Str == b.Str && a.Source == b.Source
}

func paramsEqual(a, b []index.ToolParam) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].HasDefault != b[i].HasDefault || !valueEqual(a[i].Annotation, b[i].Annotation) {
			return false
		}
	}
	return true
}
