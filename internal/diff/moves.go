package diff

import "github.com/kaeawc/evaldiff/internal/index"

// reconcileAgentMoves promotes 1:1 same-name pairs from
// (out.Removed, out.Added) into out.Modified entries with "file"
// prepended to Fields. Only agents with a literal Name kwarg are
// eligible — unnamed agents have no stable cross-file identity.
//
// 1:1 means there's exactly one Removed and one Added candidate with
// the same Name. When more than one candidate exists on either side,
// the matching is ambiguous and we leave the entries as remove + add
// rather than guess.
func reconcileAgentMoves(out *AgentChanges) {
	addedByName := groupAgentsByName(out.Added)
	removedByName := groupAgentsByName(out.Removed)
	addedDrop, removedDrop := map[int]bool{}, map[int]bool{}
	for name, addedIxes := range addedByName {
		if len(addedIxes) != 1 {
			continue
		}
		removedIxes, ok := removedByName[name]
		if !ok || len(removedIxes) != 1 {
			continue
		}
		ai, ri := addedIxes[0], removedIxes[0]
		addedDrop[ai] = true
		removedDrop[ri] = true
		out.Modified = append(out.Modified, makeAgentMove(out.Removed[ri], out.Added[ai]))
	}
	if len(addedDrop) == 0 {
		return
	}
	out.Added = filterAgents(out.Added, addedDrop)
	out.Removed = filterAgents(out.Removed, removedDrop)
}

func reconcileToolMoves(out *ToolChanges) {
	addedByName := groupToolsByName(out.Added)
	removedByName := groupToolsByName(out.Removed)
	addedDrop, removedDrop := map[int]bool{}, map[int]bool{}
	for name, addedIxes := range addedByName {
		if len(addedIxes) != 1 {
			continue
		}
		removedIxes, ok := removedByName[name]
		if !ok || len(removedIxes) != 1 {
			continue
		}
		ai, ri := addedIxes[0], removedIxes[0]
		addedDrop[ai] = true
		removedDrop[ri] = true
		out.Modified = append(out.Modified, makeToolMove(out.Removed[ri], out.Added[ai]))
	}
	if len(addedDrop) == 0 {
		return
	}
	out.Added = filterTools(out.Added, addedDrop)
	out.Removed = filterTools(out.Removed, removedDrop)
}

func groupAgentsByName(in []index.Agent) map[string][]int {
	out := map[string][]int{}
	for i, a := range in {
		if !a.Name.IsLiteral() {
			continue
		}
		out[a.Name.Str] = append(out[a.Name.Str], i)
	}
	return out
}

func groupToolsByName(in []index.Tool) map[string][]int {
	out := map[string][]int{}
	for i, t := range in {
		out[t.Name] = append(out[t.Name], i)
	}
	return out
}

func filterAgents(in []index.Agent, drop map[int]bool) []index.Agent {
	out := make([]index.Agent, 0, len(in)-len(drop))
	for i, a := range in {
		if drop[i] {
			continue
		}
		out = append(out, a)
	}
	return out
}

func filterTools(in []index.Tool, drop map[int]bool) []index.Tool {
	out := make([]index.Tool, 0, len(in)-len(drop))
	for i, t := range in {
		if drop[i] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// makeAgentMove builds a Modified entry for a cross-file move. "file"
// always leads Fields so PR-comment renderers can show the move
// prominently; remaining fields fall through agentFieldDiff so per-
// kwarg edits done as part of the move surface too.
func makeAgentMove(b, a index.Agent) AgentMod {
	fields := append([]string{"file"}, agentFieldDiff(b, a)...)
	mod := AgentMod{Before: b, After: a, Fields: fields}
	if containsField(fields, "system") {
		mod.SystemDiff = literalTextDiff(b.System, a.System)
	}
	return mod
}

func makeToolMove(b, a index.Tool) ToolMod {
	fields := append([]string{"file"}, toolFieldDiff(b, a)...)
	mod := ToolMod{Before: b, After: a, Fields: fields}
	if containsField(fields, "description") {
		mod.DescriptionDiff = literalTextDiff(b.Description, a.Description)
	}
	if containsField(fields, "params") {
		mod.ParamsDiff = paramsStructuralDiff(b.Params, a.Params)
	}
	return mod
}
