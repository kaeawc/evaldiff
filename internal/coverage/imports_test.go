package coverage

import (
	"context"
	"reflect"
	"testing"
)

func TestExtractImports(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []Import
	}{
		{
			name: "plain import",
			src:  "import x\n",
			want: []Import{{Module: "x"}},
		},
		{
			name: "dotted module import",
			src:  "import a.b.c\n",
			want: []Import{{Module: "a.b.c"}},
		},
		{
			name: "aliased import",
			src:  "import claude_agent_sdk as claude\n",
			want: []Import{{Module: "claude_agent_sdk", ModuleAlias: "claude"}},
		},
		{
			name: "comma-separated imports",
			src:  "import a, b.c, d as e\n",
			want: []Import{
				{Module: "a"},
				{Module: "b.c"},
				{Module: "d", ModuleAlias: "e"},
			},
		},
		{
			name: "from X import names",
			src:  "from app.agents import researcher, writer\n",
			want: []Import{{
				Module: "app.agents",
				Items:  []ImportItem{{Name: "researcher"}, {Name: "writer"}},
			}},
		},
		{
			name: "from X import a as b",
			src:  "from app.agents import researcher as r\n",
			want: []Import{{
				Module: "app.agents",
				Items:  []ImportItem{{Name: "researcher", Alias: "r"}},
			}},
		},
		{
			name: "from X import * (wildcard)",
			src:  "from app.tools import *\n",
			want: []Import{{Module: "app.tools"}},
		},
		{
			name: "imports inside function body are ignored",
			src: `def helper():
    import lazy_thing

import always
`,
			want: []Import{{Module: "always"}},
		},
		{
			name: "imports inside if/try are ignored",
			src: `if True:
    import sometimes

import always
`,
			want: []Import{{Module: "always"}},
		},
		{
			name: "no imports",
			src:  "x = 1\n",
			want: nil,
		},
		{
			name: "mixed import block",
			src: `import os
from claude_agent_sdk import Agent, tool
from .helpers import build
`,
			// Last entry has dot-prefixed module from the relative_import node.
			want: []Import{
				{Module: "os"},
				{Module: "claude_agent_sdk", Items: []ImportItem{{Name: "Agent"}, {Name: "tool"}}},
				{Module: ".helpers", Items: []ImportItem{{Name: "build"}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractImports(context.Background(), []byte(tt.src))
			if err != nil {
				t.Fatalf("ExtractImports: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("imports mismatch:\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}
