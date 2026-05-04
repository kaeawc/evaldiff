package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/kaeawc/evaldiff/internal/vfs"
)

func TestBuild_ExtractsAgentsAndToolsAcrossFiles(t *testing.T) {
	fs := vfs.NewMem(map[string]string{
		"/repo/agent.py": `Agent(model="claude-sonnet-4-6", system="hi")
`,
		"/repo/tools.py": `@tool
def search(q: str):
    """Search."""
`,
		"/repo/README.md": "ignored",
	})

	idx, err := Build(context.Background(), fs, "/repo")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if idx.Root != "/repo" {
		t.Fatalf("Root = %q, want /repo", idx.Root)
	}
	if len(idx.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(idx.Files))
	}
	if idx.Files[0].File != "agent.py" || idx.Files[1].File != "tools.py" {
		t.Fatalf("file order/names: %v", []string{idx.Files[0].File, idx.Files[1].File})
	}

	a := idx.Files[0]
	if len(a.Agents) != 1 || a.Agents[0].Model.Str != "claude-sonnet-4-6" {
		t.Fatalf("agent.py agents: %+v", a.Agents)
	}
	if len(a.Tools) != 0 {
		t.Fatalf("agent.py tools: %+v", a.Tools)
	}
	if a.Agents[0].File != "agent.py" {
		t.Fatalf("Agent.File = %q, want relative path agent.py", a.Agents[0].File)
	}

	tEntry := idx.Files[1]
	if len(tEntry.Tools) != 1 || tEntry.Tools[0].Name != "search" {
		t.Fatalf("tools.py tools: %+v", tEntry.Tools)
	}
	if tEntry.Tools[0].File != "tools.py" {
		t.Fatalf("Tool.File = %q, want tools.py", tEntry.Tools[0].File)
	}
	if len(a.Hash) != 64 {
		t.Fatalf("agent.py hash should be 64 hex chars, got %q", a.Hash)
	}
}

func TestBuild_EmptyTree(t *testing.T) {
	fs := vfs.NewMem(map[string]string{})
	idx, err := Build(context.Background(), fs, "/repo")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(idx.Files) != 0 {
		t.Fatalf("Files = %v, want none", idx.Files)
	}
	h, err := idx.Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h == "" {
		t.Fatal("empty hash")
	}
}

func TestIndexHash_Deterministic(t *testing.T) {
	fs := vfs.NewMem(map[string]string{
		"/repo/a.py": `Agent(model="m1")`,
		"/repo/b.py": `Agent(model="m2")`,
	})
	first := mustHash(t, fs, "/repo")
	second := mustHash(t, fs, "/repo")
	if first != second {
		t.Fatalf("hash not deterministic:\n first: %s\nsecond: %s", first, second)
	}
}

func TestIndexHash_ChangesWhenBehaviorChanges(t *testing.T) {
	before := mustHash(t, vfs.NewMem(map[string]string{
		"/repo/a.py": `Agent(model="m1")`,
	}), "/repo")
	after := mustHash(t, vfs.NewMem(map[string]string{
		"/repo/a.py": `Agent(model="m2")`,
	}), "/repo")
	if before == after {
		t.Fatalf("hash unchanged across model change: %s", before)
	}
}

func TestIndexHash_IgnoresRoot(t *testing.T) {
	src := map[string]string{"a.py": `Agent(model="m")`}
	left := mustHash(t, vfs.NewMem(prefix("/repo/", src)), "/repo")
	right := mustHash(t, vfs.NewMem(prefix("/elsewhere/", src)), "/elsewhere")
	if left != right {
		t.Fatalf("hash should ignore Root:\n left:  %s\n right: %s", left, right)
	}
}

func TestFileEntry_HashMatchesSourceSha256(t *testing.T) {
	const src = "x = 1\n"
	fs := vfs.NewMem(map[string]string{"/repo/a.py": src})
	idx, err := Build(context.Background(), fs, "/repo")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	sum := sha256.Sum256([]byte(src))
	want := hex.EncodeToString(sum[:])
	if got := idx.Files[0].Hash; got != want {
		t.Fatalf("FileEntry.Hash = %q, want %q", got, want)
	}
}

func TestExtractFile_SetsFileFieldOnNestedRecords(t *testing.T) {
	entry, err := extractFile(context.Background(), "subdir/x.py", []byte(`@tool
def t(): """t."""

Agent(model="m")
`))
	if err != nil {
		t.Fatalf("extractFile: %v", err)
	}
	if !reflect.DeepEqual(entry.File, "subdir/x.py") {
		t.Fatalf("entry.File = %q", entry.File)
	}
	if entry.Agents[0].File != "subdir/x.py" || entry.Tools[0].File != "subdir/x.py" {
		t.Fatalf("nested File fields not propagated: agent=%q tool=%q",
			entry.Agents[0].File, entry.Tools[0].File)
	}
}

func mustHash(t *testing.T, fs vfs.FS, root string) string {
	t.Helper()
	idx, err := Build(context.Background(), fs, root)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	h, err := idx.Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	return h
}

func prefix(p string, in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[p+k] = v
	}
	return out
}
