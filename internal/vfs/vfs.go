// Package vfs is a thin filesystem abstraction so the indexer can be tested
// with in-memory trees instead of touching disk.
package vfs

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FS is the minimum filesystem surface evaldiff needs: read a file by path
// and walk Python source files under a root.
type FS interface {
	Read(path string) ([]byte, error)
	WalkPython(root string, fn func(path string) error) error
}

// OS is a real-disk implementation of FS.
type OS struct{}

func (OS) Read(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WalkPython walks root and calls fn for every regular .py file under it,
// skipping hidden directories and common virtualenv / build dirs that almost
// never contain first-party eval code.
func (OS) WalkPython(root string, fn func(path string) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name(), path == root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if filepath.Ext(path) != ".py" {
			return nil
		}
		return fn(path)
	})
}

func shouldSkipDir(name string, isRoot bool) bool {
	if isRoot {
		return false
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "__pycache__", "node_modules", "venv", ".venv", "env", "build", "dist":
		return true
	}
	return false
}
