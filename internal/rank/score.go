package rank

import "github.com/kaeawc/evaldiff/internal/diff"

// scoreChange returns the per-behavior risk weight for one change.
// Roughly: 1.0 means "one full opaque field changed at maximum
// resolution"; text-field edits scale to 0..1 by edit ratio; param
// changes accumulate per-param.
//
// Concrete formulas:
//
//	ChangeAdded:    1.0 (whole behavior is new)
//	ChangeModified, agent:
//	   sum over changed Fields:
//	      system     → byte-level edit ratio of SystemDiff (0..1)
//	      model / constructor / tools  → 1.0
//	ChangeModified, tool:
//	   sum over changed Fields:
//	      description → byte-level edit ratio of DescriptionDiff (0..1)
//	      params      → count of added + removed + modified params
//	      name / schema → 1.0
//
// These weights are deliberately conservative and easy to tune. The
// invariant downstream callers rely on is: a larger score means a
// bigger semantic change, all else being equal.
func scoreChange(c BehaviorChange) float64 {
	switch c.Kind {
	case ChangeAdded:
		return 1.0
	case ChangeModified:
		if c.AgentMod != nil {
			return scoreAgentMod(c.AgentMod)
		}
		if c.ToolMod != nil {
			return scoreToolMod(c.ToolMod)
		}
	case ChangeRemoved:
		return 1.0
	}
	return 0
}

func scoreAgentMod(m *diff.AgentMod) float64 {
	var s float64
	for _, f := range m.Fields {
		switch f {
		case "system":
			s += textEditRatio(m.SystemDiff)
		default:
			s += 1.0
		}
	}
	return s
}

func scoreToolMod(m *diff.ToolMod) float64 {
	var s float64
	for _, f := range m.Fields {
		switch f {
		case "description":
			s += textEditRatio(m.DescriptionDiff)
		case "params":
			if m.ParamsDiff != nil {
				s += float64(len(m.ParamsDiff.Added) + len(m.ParamsDiff.Removed) + len(m.ParamsDiff.Modified))
			} else {
				s += 1.0
			}
		default:
			s += 1.0
		}
	}
	return s
}

// textEditRatio returns the fraction of bytes in td that aren't part of
// an Equal segment. Range [0, 1]. A nil TextDiff means "field changed
// but we couldn't render the delta" (e.g. literal → dynamic transition);
// we conservatively treat that as a full change.
func textEditRatio(td *diff.TextDiff) float64 {
	if td == nil {
		return 1.0
	}
	var edits, total int
	for _, seg := range td.Segments {
		n := len(seg.Text)
		total += n
		if seg.Kind != diff.SegmentEqual {
			edits += n
		}
	}
	if total == 0 {
		return 0
	}
	return float64(edits) / float64(total)
}
