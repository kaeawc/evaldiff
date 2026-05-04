// Package coverage builds the eval-coverage index: for every pytest-style
// test discovered under a source root, it produces a TestEntry recording
// the test's location and (eventually) the agents/tools it exercises.
//
// This first slice handles discovery only. The Touches field on each
// TestEntry is reserved for later slices that map tests back to behaviors
// in the index.
package coverage

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kaeawc/evaldiff/internal/vfs"
)

// Coverage is the discovered test catalog for one source tree. Tests is
// sorted by (file, line) so two builds over the same input produce
// identical JSON.
type Coverage struct {
	Root  string      `json:"root"`
	Tests []TestEntry `json:"tests"`
}

// TestEntry describes one statically-discovered test. Class is empty for
// top-level test functions and set to the enclosing class name for class
// methods (pytest collects both).
type TestEntry struct {
	File    string        `json:"file"`
	Line    int           `json:"line"`
	Name    string        `json:"name"`
	Class   string        `json:"class,omitempty"`
	Touches []BehaviorRef `json:"touches,omitempty"`
}

// BehaviorRef points at one Agent or Tool in the behavior index. The
// shape is reserved for slice 2b which populates TestEntry.Touches; this
// slice keeps the type defined so the JSON schema is stable from 2a
// onward.
type BehaviorRef struct {
	Kind string `json:"kind"` // "agent" or "tool"
	File string `json:"file"`
	Name string `json:"name"`
}

// Build walks every Python file under root via fs, filters to pytest-style
// test files (test_*.py or *_test.py), extracts test functions and class
// methods, and returns the merged Coverage. File paths in the result are
// relative to root.
func Build(ctx context.Context, fs vfs.FS, root string) (*Coverage, error) {
	cov := &Coverage{Root: root}
	err := fs.WalkPython(root, func(absPath string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !IsTestFile(absPath) {
			return nil
		}
		src, err := fs.Read(absPath)
		if err != nil {
			return fmt.Errorf("read %s: %w", absPath, err)
		}
		rel, err := filepath.Rel(root, absPath)
		if err != nil {
			rel = absPath
		}
		rel = filepath.ToSlash(rel)
		tests, err := ExtractTests(ctx, rel, src)
		if err != nil {
			return fmt.Errorf("extract %s: %w", absPath, err)
		}
		cov.Tests = append(cov.Tests, tests...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(cov.Tests, func(i, j int) bool {
		if cov.Tests[i].File != cov.Tests[j].File {
			return cov.Tests[i].File < cov.Tests[j].File
		}
		return cov.Tests[i].Line < cov.Tests[j].Line
	})
	return cov, nil
}

// IsTestFile reports whether path matches pytest's default file
// discovery rules: basename starts with "test_" or ends with "_test.py".
// __init__.py files and conftest.py are excluded — they hold fixtures
// and helpers, not tests themselves.
func IsTestFile(path string) bool {
	base := filepath.Base(path)
	if base == "conftest.py" || base == "__init__.py" {
		return false
	}
	if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
		return true
	}
	if strings.HasSuffix(base, "_test.py") {
		return true
	}
	return false
}
