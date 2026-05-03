package vfs

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Mem is an in-memory FS used by tests. Paths are stored verbatim;
// WalkPython visits them in lexical order so test assertions are stable.
type Mem struct {
	files map[string][]byte
}

func NewMem(files map[string]string) *Mem {
	m := &Mem{files: make(map[string][]byte, len(files))}
	for p, c := range files {
		m.files[p] = []byte(c)
	}
	return m
}

func (m *Mem) Read(path string) ([]byte, error) {
	b, ok := m.files[path]
	if !ok {
		return nil, fmt.Errorf("vfs: file not found: %s", path)
	}
	return b, nil
}

func (m *Mem) WalkPython(root string, fn func(path string) error) error {
	root = filepath.Clean(root)
	paths := make([]string, 0, len(m.files))
	for p := range m.files {
		if filepath.Ext(p) != ".py" {
			continue
		}
		rel, ok := relUnder(p, root)
		if !ok {
			continue
		}
		if relHasSkippedDir(rel) {
			continue
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := fn(p); err != nil {
			return err
		}
	}
	return nil
}

func relUnder(path, root string) (string, bool) {
	if root == "." || root == "" {
		return path, true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	if strings.HasPrefix(rel, "..") {
		return "", false
	}
	return rel, true
}

func relHasSkippedDir(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i, p := range parts[:len(parts)-1] { // exclude the file itself
		if shouldSkipDir(p, i == 0 && p == ".") {
			return true
		}
	}
	return false
}
