package diff

import "unicode"

// SegmentKind identifies the role of a TextSegment in a TextDiff.
type SegmentKind int

const (
	SegmentEqual   SegmentKind = iota // present in both before and after
	SegmentRemoved                    // present only in before
	SegmentAdded                      // present only in after
)

// TextSegment is a contiguous run of tokens with the same SegmentKind.
// Renderers can map Equal → plain, Removed → strikethrough, Added → bold.
type TextSegment struct {
	Kind SegmentKind `json:"kind"`
	Text string      `json:"text"`
}

// TextDiff is a token-level diff over two prompt strings. Tokens are
// preserved in source form (whitespace, punctuation, words) so that
// concatenating each segment's Text in order rebuilds the original
// strings: all Equal+Removed segments yield `before`, all Equal+Added
// segments yield `after`.
type TextDiff struct {
	Segments []TextSegment `json:"segments"`
}

// TextDiffOf computes the token-level diff between before and after. It
// returns nil when the two strings are equal — callers can treat nil as
// "nothing to render."
func TextDiffOf(before, after string) *TextDiff {
	if before == after {
		return nil
	}
	a := tokenize(before)
	b := tokenize(after)
	td := &TextDiff{Segments: lcsSegments(a, b)}
	return td
}

// tokenize splits s into runs of (1) word characters, (2) whitespace, or
// (3) any other single rune (punctuation, symbols). Each run is one
// token. This keeps diffs aligned to word and punctuation boundaries
// instead of producing per-character noise.
func tokenize(s string) []string {
	var out []string
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case isWordRune(r):
			j := i + 1
			for j < len(runes) && isWordRune(runes[j]) {
				j++
			}
			out = append(out, string(runes[i:j]))
			i = j
		case unicode.IsSpace(r):
			j := i + 1
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				j++
			}
			out = append(out, string(runes[i:j]))
			i = j
		default:
			out = append(out, string(runes[i]))
			i++
		}
	}
	return out
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// lcsSegments runs an O(len(a)*len(b)) longest-common-subsequence DP to
// produce a coalesced sequence of TextSegments that, when rendered,
// reconstructs both `a` and `b` per their respective kinds.
func lcsSegments(a, b []string) []TextSegment {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	return walkLCSTable(a, b, lcsTable(a, b))
}

// lcsTable returns the suffix LCS DP table where table[i][j] is the
// longest common subsequence length of a[i:] and b[j:].
func lcsTable(a, b []string) [][]int {
	n, m := len(a), len(b)
	table := make([][]int, n+1)
	for i := range table {
		table[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
				continue
			}
			if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	return table
}

// walkLCSTable walks the DP table to produce the coalesced segment list.
func walkLCSTable(a, b []string, table [][]int) []TextSegment {
	n, m := len(a), len(b)
	var segs []TextSegment
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			segs = appendSegment(segs, SegmentEqual, a[i])
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			segs = appendSegment(segs, SegmentRemoved, a[i])
			i++
		default:
			segs = appendSegment(segs, SegmentAdded, b[j])
			j++
		}
	}
	for ; i < n; i++ {
		segs = appendSegment(segs, SegmentRemoved, a[i])
	}
	for ; j < m; j++ {
		segs = appendSegment(segs, SegmentAdded, b[j])
	}
	return segs
}

// appendSegment coalesces consecutive same-Kind tokens into one segment
// so renderers see one Added run instead of one segment per token.
func appendSegment(segs []TextSegment, kind SegmentKind, text string) []TextSegment {
	if n := len(segs); n > 0 && segs[n-1].Kind == kind {
		segs[n-1].Text += text
		return segs
	}
	return append(segs, TextSegment{Kind: kind, Text: text})
}
