package index

import (
	"context"
	"reflect"
	"testing"
)

func TestExtractAgents(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []Agent
	}{
		{
			name: "bare Agent with model + system literals",
			src: `from claude_agent_sdk import Agent
agent = Agent(
    model="claude-sonnet-4-6",
    system="You are helpful.",
)
`,
			want: []Agent{{
				File:        "/x.py",
				Line:        2,
				Constructor: "Agent",
				Model:       Value{Kind: ValueLiteral, Str: "claude-sonnet-4-6", Source: `"claude-sonnet-4-6"`},
				System:      Value{Kind: ValueLiteral, Str: "You are helpful.", Source: `"You are helpful."`},
			}},
		},
		{
			name: "qualified call claude.Agent",
			src: `import claude_agent_sdk as claude
a = claude.Agent(model="claude-opus-4-7")
`,
			want: []Agent{{
				File:        "/x.py",
				Line:        2,
				Constructor: "claude.Agent",
				Model:       Value{Kind: ValueLiteral, Str: "claude-opus-4-7", Source: `"claude-opus-4-7"`},
			}},
		},
		{
			name: "instructions kwarg fills System slot when system absent",
			src: `Agent(model="m", instructions="be brief")
`,
			want: []Agent{{
				File:        "/x.py",
				Line:        1,
				Constructor: "Agent",
				Model:       Value{Kind: ValueLiteral, Str: "m", Source: `"m"`},
				System:      Value{Kind: ValueLiteral, Str: "be brief", Source: `"be brief"`},
			}},
		},
		{
			name: "system wins over instructions when both present",
			src: `Agent(model="m", system="primary", instructions="fallback")
`,
			want: []Agent{{
				File:        "/x.py",
				Line:        1,
				Constructor: "Agent",
				Model:       Value{Kind: ValueLiteral, Str: "m", Source: `"m"`},
				System:      Value{Kind: ValueLiteral, Str: "primary", Source: `"primary"`},
			}},
		},
		{
			name: "dynamic value preserves source text",
			src: `MODEL = pick_model()
Agent(model=MODEL, system=load_prompt("intro"))
`,
			want: []Agent{{
				File:        "/x.py",
				Line:        2,
				Constructor: "Agent",
				Model:       Value{Kind: ValueDynamic, Source: "MODEL"},
				System:      Value{Kind: ValueDynamic, Source: `load_prompt("intro")`},
			}},
		},
		{
			name: "tools kwarg captured as dynamic list expression",
			src: `Agent(model="m", tools=[search, browse])
`,
			want: []Agent{{
				File:        "/x.py",
				Line:        1,
				Constructor: "Agent",
				Model:       Value{Kind: ValueLiteral, Str: "m", Source: `"m"`},
				Tools:       Value{Kind: ValueDynamic, Source: "[search, browse]"},
			}},
		},
		{
			name: "multiple agents in one module",
			src: `Agent(model="a")
Agent(model="b")
`,
			want: []Agent{
				{File: "/x.py", Line: 1, Constructor: "Agent", Model: Value{Kind: ValueLiteral, Str: "a", Source: `"a"`}},
				{File: "/x.py", Line: 2, Constructor: "Agent", Model: Value{Kind: ValueLiteral, Str: "b", Source: `"b"`}},
			},
		},
		{
			name: "non-Agent calls ignored (lookalike Agent_ prefix, agent lowercase)",
			src: `Agentic(model="x")
agent(model="y")
my.agent(model="z")
`,
			want: nil,
		},
		{
			name: "triple-quoted system prompt preserved",
			src:  "Agent(model=\"m\", system=\"\"\"line one\nline two\"\"\")\n",
			want: []Agent{{
				File:        "/x.py",
				Line:        1,
				Constructor: "Agent",
				Model:       Value{Kind: ValueLiteral, Str: "m", Source: `"m"`},
				System:      Value{Kind: ValueLiteral, Str: "line one\nline two", Source: "\"\"\"line one\nline two\"\"\""},
			}},
		},
		{
			name: "name kwarg extracted (OpenAI Agents SDK shape)",
			src: `Agent(name="Translator", model="gpt-5", instructions="Translate text.")
`,
			want: []Agent{{
				File:        "/x.py",
				Line:        1,
				Constructor: "Agent",
				Name:        Value{Kind: ValueLiteral, Str: "Translator", Source: `"Translator"`},
				Model:       Value{Kind: ValueLiteral, Str: "gpt-5", Source: `"gpt-5"`},
				System:      Value{Kind: ValueLiteral, Str: "Translate text.", Source: `"Translate text."`},
			}},
		},
		{
			name: "adjacent-string concat in parens collapses to literal",
			src: `Agent(
    name="x",
    system=(
        "You are a customer support agent. "
        "Always escalate billing questions."
    ),
)
`,
			want: []Agent{{
				File:        "/x.py",
				Line:        1,
				Constructor: "Agent",
				Name:        Value{Kind: ValueLiteral, Str: "x", Source: `"x"`},
				System: Value{
					Kind:   ValueLiteral,
					Str:    "You are a customer support agent. Always escalate billing questions.",
					Source: "(\n        \"You are a customer support agent. \"\n        \"Always escalate billing questions.\"\n    )",
				},
			}},
		},
		{
			name: "no-paren concatenated_string also collapses",
			src: `Agent(name="x", system="hello " "world")
`,
			want: []Agent{{
				File:        "/x.py",
				Line:        1,
				Constructor: "Agent",
				Name:        Value{Kind: ValueLiteral, Str: "x", Source: `"x"`},
				System:      Value{Kind: ValueLiteral, Str: "hello world", Source: `"hello " "world"`},
			}},
		},
		{
			name: "parenthesized single string still extracts as literal",
			src: `Agent(name="x", system=("just one"))
`,
			want: []Agent{{
				File:        "/x.py",
				Line:        1,
				Constructor: "Agent",
				Name:        Value{Kind: ValueLiteral, Str: "x", Source: `"x"`},
				System:      Value{Kind: ValueLiteral, Str: "just one", Source: `("just one")`},
			}},
		},
		{
			name: "no Agent calls in file → empty result",
			src:  "x = 1\n",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractAgents(context.Background(), "/x.py", []byte(tt.src))
			if err != nil {
				t.Fatalf("ExtractAgents: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("agents mismatch:\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

func TestExtractAgents_PositionalArgsAreIgnored(t *testing.T) {
	// Claude Agent SDK uses kwargs by convention; positional args fall into
	// ValueMissing today. Documenting that in a test so the behavior is
	// explicit, not accidental.
	got, err := ExtractAgents(context.Background(), "/x.py", []byte(`Agent("claude-sonnet-4-6")`))
	if err != nil {
		t.Fatalf("ExtractAgents: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d agents, want 1", len(got))
	}
	if !got[0].Model.IsMissing() {
		t.Fatalf("Model = %+v, want Missing", got[0].Model)
	}
}
