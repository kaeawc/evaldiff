package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/kaeawc/evaldiff/internal/vfs"
)

// Index is the content-addressable behavior snapshot for a source tree.
// All paths in Files are relative to Root and Files is sorted lexically so
// two indexes built from the same input produce identical hashes.
type Index struct {
	Root  string      `json:"root"`
	Files []FileEntry `json:"files"`
}

// FileEntry holds the behaviors extracted from a single Python file.
// Hash is the hex sha256 of the source bytes; downstream slices use it as
// the cache key when deciding whether a per-file extraction can be reused.
type FileEntry struct {
	File   string  `json:"file"`
	Hash   string  `json:"hash"`
	Agents []Agent `json:"agents,omitempty"`
	Tools  []Tool  `json:"tools,omitempty"`
}

// Build walks every Python file under root using fs, extracts the agent and
// tool behaviors from each, and returns the combined index. File paths in
// the result are relative to root.
func Build(ctx context.Context, fs vfs.FS, root string) (*Index, error) {
	idx := &Index{Root: root}
	err := fs.WalkPython(root, func(absPath string) error {
		if err := ctx.Err(); err != nil {
			return err
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
		entry, err := extractFile(ctx, rel, src)
		if err != nil {
			return fmt.Errorf("extract %s: %w", absPath, err)
		}
		idx.Files = append(idx.Files, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(idx.Files, func(i, j int) bool { return idx.Files[i].File < idx.Files[j].File })
	return idx, nil
}

// extractFile runs every per-file extractor against src and returns one
// FileEntry. The relative path is also stamped onto each contained Agent
// and Tool so downstream stages can locate the original source.
func extractFile(ctx context.Context, relPath string, src []byte) (FileEntry, error) {
	agents, err := ExtractAgents(ctx, relPath, src)
	if err != nil {
		return FileEntry{}, err
	}
	tools, err := ExtractTools(ctx, relPath, src)
	if err != nil {
		return FileEntry{}, err
	}
	sum := sha256.Sum256(src)
	return FileEntry{
		File:   relPath,
		Hash:   hex.EncodeToString(sum[:]),
		Agents: agents,
		Tools:  tools,
	}, nil
}

// Hash returns the content-addressable identity of the index: hex sha256 of
// its canonical JSON form. Two indexes built from the same source tree
// produce the same hash; any extracted behavior change flips it.
func (i *Index) Hash() (string, error) {
	buf, err := i.canonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:]), nil
}

// canonicalJSON serializes the index with sorted map keys and no extra
// whitespace, omitting the Root field so two indexes of the same code
// laid out under different absolute paths still hash identically.
func (i *Index) canonicalJSON() ([]byte, error) {
	canon := struct {
		Files []FileEntry `json:"files"`
	}{Files: i.Files}
	return json.Marshal(canon)
}
