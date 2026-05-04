package io

import (
	"fmt"
	"strings"

	"github.com/kaeawc/evaldiff/internal/diff"
	"github.com/kaeawc/evaldiff/internal/rank"
)

// RenderMarkdown returns the EvalsAtRisk result as a markdown blob
// suitable for posting as a GitHub PR comment. Empty input still
// returns a friendly "no evals at risk" message so the renderer can
// be wired into a Github Action that always posts something.
//
// Format:
//
//	### evaldiff: N evals at risk
//
//	#### `path/to/test_file.py::test_name` — score 1.20
//	- **modified** agent `researcher` in `app/agents.py`
//	  - fields: `model`, `system`
//	  - system prompt:
//	    > You are a ~~helpful~~ **very helpful** assistant.
//	- **added** tool `browse` in `app/tools.py`
//
//	---
//
//	### Removed behaviors
//
//	Tests that imported these in base may fail to load:
//	- agent `app/agents.py::gone`
//	- tool  `app/tools.py::old_search`
func RenderMarkdown(risk *rank.EvalsAtRisk) string {
	var b strings.Builder
	if risk == nil || (len(risk.Tests) == 0 && len(risk.Removed) == 0) {
		b.WriteString("### evaldiff: no evals at risk\n")
		return b.String()
	}
	fmt.Fprintf(&b, "### evaldiff: %d %s at risk\n\n", len(risk.Tests), pluralize("eval", len(risk.Tests)))
	for _, t := range risk.Tests {
		renderTest(&b, t)
	}
	if len(risk.Removed) > 0 {
		renderRemoved(&b, risk.Removed)
	}
	return b.String()
}

func renderTest(b *strings.Builder, r rank.EvalRisk) {
	name := r.Test.Name
	if r.Test.Class != "" {
		name = r.Test.Class + "." + name
	}
	fmt.Fprintf(b, "#### `%s::%s` — score %.2f\n\n", r.Test.File, name, r.Score)
	for _, ch := range r.Affected {
		renderChange(b, ch)
	}
	b.WriteString("\n")
}

func renderChange(b *strings.Builder, ch rank.BehaviorChange) {
	fmt.Fprintf(b, "- **%s** %s `%s` in `%s`\n", string(ch.Kind), ch.Ref.Kind, ch.Ref.Name, ch.Ref.File)
	switch {
	case ch.AgentMod != nil:
		renderAgentMod(b, ch.AgentMod)
	case ch.ToolMod != nil:
		renderToolMod(b, ch.ToolMod)
	}
}

func renderAgentMod(b *strings.Builder, m *diff.AgentMod) {
	fmt.Fprintf(b, "  - fields: %s\n", joinBackticked(m.Fields))
	if m.SystemDiff != nil {
		b.WriteString("  - system prompt:\n")
		writeBlockquote(b, "    ", renderTextDiff(m.SystemDiff))
	}
}

func renderToolMod(b *strings.Builder, m *diff.ToolMod) {
	fmt.Fprintf(b, "  - fields: %s\n", joinBackticked(m.Fields))
	if m.DescriptionDiff != nil {
		b.WriteString("  - description:\n")
		writeBlockquote(b, "    ", renderTextDiff(m.DescriptionDiff))
	}
	if m.ParamsDiff != nil {
		b.WriteString("  - params: ")
		b.WriteString(renderParamsDiff(m.ParamsDiff))
		b.WriteString("\n")
	}
}

// renderTextDiff turns a token-level TextDiff into inline markdown:
// removed runs get strikethrough (~~text~~), added runs get bold
// (**text**), equal runs are plain. Newlines are preserved so the
// caller can wrap the whole blob in a blockquote.
func renderTextDiff(td *diff.TextDiff) string {
	var b strings.Builder
	for _, seg := range td.Segments {
		switch seg.Kind {
		case diff.SegmentEqual:
			b.WriteString(seg.Text)
		case diff.SegmentRemoved:
			b.WriteString("~~")
			b.WriteString(seg.Text)
			b.WriteString("~~")
		case diff.SegmentAdded:
			b.WriteString("**")
			b.WriteString(seg.Text)
			b.WriteString("**")
		}
	}
	return b.String()
}

// renderParamsDiff produces a one-line summary like "+`lang`, +`page_size`, -`limit`, ~`q` (annotation)".
func renderParamsDiff(p *diff.ParamsDiff) string {
	parts := []string{}
	for _, a := range p.Added {
		parts = append(parts, "+`"+a.Name+"`")
	}
	for _, r := range p.Removed {
		parts = append(parts, "-`"+r.Name+"`")
	}
	for _, m := range p.Modified {
		parts = append(parts, "~`"+m.Before.Name+"` ("+strings.Join(m.Fields, ", ")+")")
	}
	if len(parts) == 0 {
		return "_(reorder only)_"
	}
	return strings.Join(parts, ", ")
}

func renderRemoved(b *strings.Builder, refs []rank.BehaviorRef) {
	b.WriteString("---\n\n### Removed behaviors\n\n")
	b.WriteString("Tests that imported these in base may fail to load:\n\n")
	for _, r := range refs {
		fmt.Fprintf(b, "- %s `%s::%s`\n", r.Kind, r.File, r.Name)
	}
}

// writeBlockquote prefixes every line of body with `> ` and writes the
// result to b. Each prefixed line itself starts with the caller's
// indent (e.g. "    " for a list-item-nested blockquote) so PR comment
// renderers keep the quote attached to the right list level.
func writeBlockquote(b *strings.Builder, indent, body string) {
	for _, line := range strings.Split(body, "\n") {
		b.WriteString(indent)
		b.WriteString("> ")
		b.WriteString(line)
		b.WriteString("\n")
	}
}

func joinBackticked(in []string) string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = "`" + s + "`"
	}
	return strings.Join(out, ", ")
}

func pluralize(noun string, n int) string {
	if n == 1 {
		return noun
	}
	return noun + "s"
}
