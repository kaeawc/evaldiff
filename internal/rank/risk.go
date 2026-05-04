// Package rank turns a behavior diff plus an eval-coverage index into the
// answer evaldiff was built for: which evals are at risk because something
// they touch changed.
//
// This first slice handles the intersection: for every test in the head
// coverage, it records which Added or Modified behaviors that test touches
// and at which kind of change. Removed behaviors are surfaced as a
// top-level list — they don't appear in head coverage's Touches because
// they no longer exist, but they're still useful context for reviewers.
//
// Ranking by edit distance / structural-diff magnitude lands in 4b.
package rank

import (
	"sort"

	"github.com/kaeawc/evaldiff/internal/coverage"
	"github.com/kaeawc/evaldiff/internal/diff"
	"github.com/kaeawc/evaldiff/internal/index"
)

// EvalsAtRisk is the per-PR result evaldiff returns. Tests is sorted
// (file, line) so JSON output is byte-stable. Removed lists behaviors
// that vanished in head — tests that touched them in base coverage may
// fail to import.
type EvalsAtRisk struct {
	Tests   []EvalRisk    `json:"tests,omitempty"`
	Removed []BehaviorRef `json:"removed_behaviors,omitempty"`
}

// EvalRisk pairs a test with the changed behaviors it touches. A test
// appears here at most once even if it touches several changed behaviors.
// Score is the sum of per-Affected BehaviorChange scores; EvalsAtRisk
// sorts Tests by Score descending.
type EvalRisk struct {
	Test     coverage.TestEntry `json:"test"`
	Affected []BehaviorChange   `json:"affected"`
	Score    float64            `json:"score"`
}

// BehaviorChange is one (Ref, Kind) pair: which behavior changed and how.
// Score quantifies the change magnitude (see score.go for the formula).
// AgentMod / ToolMod carry the original diff entry so PR-comment
// renderers can show the prompt or schema delta without re-diffing.
type BehaviorChange struct {
	Ref      BehaviorRef    `json:"ref"`
	Kind     ChangeKind     `json:"kind"`
	Score    float64        `json:"score"`
	AgentMod *diff.AgentMod `json:"agent_mod,omitempty"`
	ToolMod  *diff.ToolMod  `json:"tool_mod,omitempty"`
}

// BehaviorRef mirrors coverage.BehaviorRef shape. It's repeated here so
// rank doesn't force every consumer to import coverage just for the type.
type BehaviorRef struct {
	Kind string `json:"kind"`
	File string `json:"file"`
	Name string `json:"name"`
}

// ChangeKind discriminates added vs modified vs removed behaviors.
type ChangeKind string

const (
	ChangeAdded    ChangeKind = "added"
	ChangeModified ChangeKind = "modified"
	ChangeRemoved  ChangeKind = "removed"
)

// Compute intersects the changeset with head coverage and returns the
// at-risk eval set. The headIdx is needed to derive each agent's
// BehaviorRef.Name (literal name preferred, else constructor#ordinal),
// which must match the identity coverage.AttachTouches stamped onto
// each test's Touches.
func Compute(cs *diff.Changeset, headIdx *index.Index, headCov *coverage.Coverage) *EvalsAtRisk {
	out := &EvalsAtRisk{}
	if cs == nil || headIdx == nil || headCov == nil {
		return out
	}
	changesByRef := indexChanges(cs, headIdx)
	for _, test := range headCov.Tests {
		risk := matchTest(test, changesByRef)
		if len(risk.Affected) > 0 {
			out.Tests = append(out.Tests, risk)
		}
	}
	out.Removed = removedBehaviors(cs)
	sort.Slice(out.Tests, func(i, j int) bool {
		if out.Tests[i].Score != out.Tests[j].Score {
			return out.Tests[i].Score > out.Tests[j].Score
		}
		// Stable tiebreaker: file then line.
		if out.Tests[i].Test.File != out.Tests[j].Test.File {
			return out.Tests[i].Test.File < out.Tests[j].Test.File
		}
		return out.Tests[i].Test.Line < out.Tests[j].Test.Line
	})
	return out
}

func matchTest(test coverage.TestEntry, changes map[coverage.BehaviorRef][]BehaviorChange) EvalRisk {
	risk := EvalRisk{Test: test}
	for _, touch := range test.Touches {
		if matched, ok := changes[touch]; ok {
			risk.Affected = append(risk.Affected, matched...)
		}
	}
	for i := range risk.Affected {
		risk.Affected[i].Score = scoreChange(risk.Affected[i])
		risk.Score += risk.Affected[i].Score
	}
	return risk
}

// indexChanges produces, for every Added or Modified behavior in the
// changeset, the BehaviorRef coverage would have stamped onto a test's
// Touches list. Removed entries are excluded — they no longer exist in
// head, so head coverage cannot reference them.
//
// Agent identity needs the head index to derive the ordinal for unnamed
// agents. We build a (file, line) → ordinal map from headIdx in one pass
// because diff Agent records carry both fields verbatim.
func indexChanges(cs *diff.Changeset, headIdx *index.Index) map[coverage.BehaviorRef][]BehaviorChange {
	posToRef := buildAgentPosIndex(headIdx)
	out := map[coverage.BehaviorRef][]BehaviorChange{}

	for i := range cs.Agents.Added {
		a := cs.Agents.Added[i]
		if ref, ok := posToRef[posKey{a.File, a.Line}]; ok {
			out[ref] = append(out[ref], BehaviorChange{Ref: toRankRef(ref), Kind: ChangeAdded})
		}
	}
	for i := range cs.Agents.Modified {
		m := &cs.Agents.Modified[i]
		if ref, ok := posToRef[posKey{m.After.File, m.After.Line}]; ok {
			out[ref] = append(out[ref], BehaviorChange{Ref: toRankRef(ref), Kind: ChangeModified, AgentMod: m})
		}
	}
	for _, t := range cs.Tools.Added {
		ref := coverage.BehaviorRef{Kind: "tool", File: t.File, Name: t.Name}
		out[ref] = append(out[ref], BehaviorChange{Ref: toRankRef(ref), Kind: ChangeAdded})
	}
	for i := range cs.Tools.Modified {
		m := &cs.Tools.Modified[i]
		ref := coverage.BehaviorRef{Kind: "tool", File: m.After.File, Name: m.After.Name}
		out[ref] = append(out[ref], BehaviorChange{Ref: toRankRef(ref), Kind: ChangeModified, ToolMod: m})
	}
	return out
}

// posKey identifies an agent by (file, line) which is unique within one
// build of the index — there's at most one call expression on a given
// line of a given file.
type posKey struct {
	file string
	line int
}

func buildAgentPosIndex(idx *index.Index) map[posKey]coverage.BehaviorRef {
	out := map[posKey]coverage.BehaviorRef{}
	for _, fe := range idx.Files {
		for i, a := range fe.Agents {
			out[posKey{a.File, a.Line}] = coverage.BehaviorRef{
				Kind: "agent",
				File: a.File,
				Name: coverage.AgentRefName(a, i),
			}
		}
	}
	return out
}

// removedBehaviors collects BehaviorRefs for agents/tools that were in
// base but not head. For removed agents we can't derive a stable ordinal
// against headIdx (they don't exist there), so unnamed removed agents
// are reported with constructor#? as a placeholder; named agents and
// all tools surface cleanly.
func removedBehaviors(cs *diff.Changeset) []BehaviorRef {
	var out []BehaviorRef
	for _, a := range cs.Agents.Removed {
		out = append(out, BehaviorRef{Kind: "agent", File: a.File, Name: removedAgentName(a)})
	}
	for _, t := range cs.Tools.Removed {
		out = append(out, BehaviorRef{Kind: "tool", File: t.File, Name: t.Name})
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

func removedAgentName(a index.Agent) string {
	if a.Name.IsLiteral() {
		return a.Name.Str
	}
	return a.Constructor + "#?"
}

func toRankRef(r coverage.BehaviorRef) BehaviorRef {
	return BehaviorRef{Kind: r.Kind, File: r.File, Name: r.Name}
}
