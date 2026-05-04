package index

import (
	"context"
	"reflect"
	"testing"
)

func TestExtractTools(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []Tool
	}{
		{
			name: "bare @tool with docstring + typed params",
			src: `from claude_agent_sdk import tool

@tool
def search(query: str, limit: int = 10) -> list[str]:
    """Search the web."""
    ...
`,
			want: []Tool{{
				File:        "/x.py",
				Line:        3,
				Name:        "search",
				Description: Value{Kind: ValueLiteral, Str: "Search the web.", Source: `"""Search the web."""`},
				Params: []ToolParam{
					{Name: "query", Annotation: Value{Kind: ValueDynamic, Source: "str"}},
					{Name: "limit", Annotation: Value{Kind: ValueDynamic, Source: "int"}, HasDefault: true},
				},
			}},
		},
		{
			name: "@tool(name=..., description=...) overrides function name + docstring",
			src: `@tool(name="web_search", description="Search the web for a query.")
def search(q: str):
    """Old docstring."""
    ...
`,
			want: []Tool{{
				File:        "/x.py",
				Line:        1,
				Name:        "web_search",
				Description: Value{Kind: ValueLiteral, Str: "Search the web for a query.", Source: `"Search the web for a query."`},
				Params: []ToolParam{
					{Name: "q", Annotation: Value{Kind: ValueDynamic, Source: "str"}},
				},
			}},
		},
		{
			name: "qualified @claude.tool",
			src: `import claude_agent_sdk as claude

@claude.tool
def ping():
    """pong."""
    ...
`,
			want: []Tool{{
				File:        "/x.py",
				Line:        3,
				Name:        "ping",
				Description: Value{Kind: ValueLiteral, Str: "pong.", Source: `"""pong."""`},
			}},
		},
		{
			name: "method inside class is skipped",
			src: `class Toolbox:
    @tool
    def search(self, q: str):
        """skipped."""
        ...
`,
			want: nil,
		},
		{
			name: "no docstring → Description is missing",
			src: `@tool
def noisy():
    print("hi")
`,
			want: []Tool{{
				File: "/x.py",
				Line: 1,
				Name: "noisy",
			}},
		},
		{
			name: "param without annotation has missing annotation",
			src: `@tool
def f(a, b: str):
    """doc."""
`,
			want: []Tool{{
				File:        "/x.py",
				Line:        1,
				Name:        "f",
				Description: Value{Kind: ValueLiteral, Str: "doc.", Source: `"""doc."""`},
				Params: []ToolParam{
					{Name: "a"},
					{Name: "b", Annotation: Value{Kind: ValueDynamic, Source: "str"}},
				},
			}},
		},
		{
			name: "@tool stacked with other decorators is still matched",
			src: `@trace
@tool
def hello():
    """hi."""
`,
			want: []Tool{{
				File:        "/x.py",
				Line:        1,
				Name:        "hello",
				Description: Value{Kind: ValueLiteral, Str: "hi.", Source: `"""hi."""`},
			}},
		},
		{
			name: "two tools in one module",
			src: `@tool
def a():
    """A."""

@tool
def b():
    """B."""
`,
			want: []Tool{
				{File: "/x.py", Line: 1, Name: "a", Description: Value{Kind: ValueLiteral, Str: "A.", Source: `"""A."""`}},
				{File: "/x.py", Line: 5, Name: "b", Description: Value{Kind: ValueLiteral, Str: "B.", Source: `"""B."""`}},
			},
		},
		{
			name: "non-tool decorator is ignored",
			src: `@cache
def helper():
    """not a tool."""
`,
			want: nil,
		},
		{
			name: "bare @function_tool extracted (OpenAI Agents SDK)",
			src: `from agents import function_tool

@function_tool
def get_weather(city: str) -> str:
    """Look up weather."""
    ...
`,
			want: []Tool{{
				File:        "/x.py",
				Line:        3,
				Name:        "get_weather",
				Description: Value{Kind: ValueLiteral, Str: "Look up weather.", Source: `"""Look up weather."""`},
				Params: []ToolParam{
					{Name: "city", Annotation: Value{Kind: ValueDynamic, Source: "str"}},
				},
			}},
		},
		{
			name: "@function_tool with name_override / description_override kwargs",
			src: `@function_tool(name_override="weather", description_override="Look up the current weather.")
def get_weather(city: str):
    """fallback docstring."""
    ...
`,
			want: []Tool{{
				File:        "/x.py",
				Line:        1,
				Name:        "weather",
				Description: Value{Kind: ValueLiteral, Str: "Look up the current weather.", Source: `"Look up the current weather."`},
				Params: []ToolParam{
					{Name: "city", Annotation: Value{Kind: ValueDynamic, Source: "str"}},
				},
			}},
		},
		{
			name: "@agents.function_tool qualified",
			src: `import agents

@agents.function_tool
def helper():
    """help."""
`,
			want: []Tool{{
				File:        "/x.py",
				Line:        3,
				Name:        "helper",
				Description: Value{Kind: ValueLiteral, Str: "help.", Source: `"""help."""`},
			}},
		},
		{
			name: "lowercase agent / Tool capital don't match",
			src: `@Tool
def x():
    """doc."""

@toolbox
def y():
    """doc."""
`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractTools(context.Background(), "/x.py", []byte(tt.src))
			if err != nil {
				t.Fatalf("ExtractTools: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tools mismatch:\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}
