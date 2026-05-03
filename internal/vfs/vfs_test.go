package vfs

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestMemWalkPython_MatchesOSFiltering(t *testing.T) {
	fs := NewMem(map[string]string{
		"/repo/a.py":             "x = 1",
		"/repo/sub/b.py":         "y = 2",
		"/repo/sub/c.txt":        "ignore me",
		"/other/d.py":            "outside root",
		"/repo/.hidden/skip.py":  "hidden dir",
		"/repo/.venv/skip.py":    "venv",
		"/repo/__pycache__/x.py": "cache",
	})

	var got []string
	if err := fs.WalkPython("/repo", func(p string) error {
		got = append(got, p)
		return nil
	}); err != nil {
		t.Fatalf("WalkPython: %v", err)
	}

	want := []string{"/repo/a.py", "/repo/sub/b.py"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestMemRead_Roundtrip(t *testing.T) {
	fs := NewMem(map[string]string{"/x.py": "hello"})
	b, err := fs.Read("/x.py")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(b) != "hello" {
		t.Fatalf("got %q want %q", b, "hello")
	}
	if _, err := fs.Read("/missing"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestOSWalkPython_SkipsHiddenAndBuildDirs(t *testing.T) {
	root := t.TempDir()

	mustWrite(t, filepath.Join(root, "keep.py"), "x = 1")
	mustWrite(t, filepath.Join(root, "pkg", "deep.py"), "y = 2")
	mustWrite(t, filepath.Join(root, ".venv", "skip.py"), "z = 3")
	mustWrite(t, filepath.Join(root, "node_modules", "skip.py"), "z = 4")
	mustWrite(t, filepath.Join(root, "build", "skip.py"), "z = 5")
	mustWrite(t, filepath.Join(root, "__pycache__", "skip.py"), "z = 6")
	mustWrite(t, filepath.Join(root, ".hidden", "skip.py"), "z = 7")
	mustWrite(t, filepath.Join(root, "notes.txt"), "ignore")

	var got []string
	if err := (OS{}).WalkPython(root, func(p string) error {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		t.Fatalf("WalkPython: %v", err)
	}
	sort.Strings(got)

	want := []string{"keep.py", "pkg/deep.py"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
