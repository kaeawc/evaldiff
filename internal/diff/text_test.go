package diff

import (
	"reflect"
	"strings"
	"testing"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"hello", []string{"hello"}},
		{"hello world", []string{"hello", " ", "world"}},
		{"Hello, world!", []string{"Hello", ",", " ", "world", "!"}},
		{"snake_case", []string{"snake_case"}},
		{"a  b", []string{"a", "  ", "b"}},
		{"123abc", []string{"123abc"}},
		{"naïve résumé", []string{"naïve", " ", "résumé"}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := tokenize(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("tokenize(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestTextDiffOf_EqualReturnsNil(t *testing.T) {
	if got := TextDiffOf("same", "same"); got != nil {
		t.Fatalf("TextDiffOf identical = %+v, want nil", got)
	}
}

func TestTextDiffOf_AddedWordInMiddle(t *testing.T) {
	got := TextDiffOf("You are a helpful assistant.", "You are a very helpful assistant.")
	wantSegs := []TextSegment{
		{Kind: SegmentEqual, Text: "You are a "},
		{Kind: SegmentAdded, Text: "very "},
		{Kind: SegmentEqual, Text: "helpful assistant."},
	}
	if !reflect.DeepEqual(got.Segments, wantSegs) {
		t.Fatalf("segments mismatch:\n got: %+v\nwant: %+v", got.Segments, wantSegs)
	}
	assertReconstructs(t, got, "You are a helpful assistant.", "You are a very helpful assistant.")
}

func TestTextDiffOf_RemovedWord(t *testing.T) {
	got := TextDiffOf("You are a very helpful assistant.", "You are a helpful assistant.")
	wantSegs := []TextSegment{
		{Kind: SegmentEqual, Text: "You are a "},
		{Kind: SegmentRemoved, Text: "very "},
		{Kind: SegmentEqual, Text: "helpful assistant."},
	}
	if !reflect.DeepEqual(got.Segments, wantSegs) {
		t.Fatalf("segments mismatch:\n got: %+v\nwant: %+v", got.Segments, wantSegs)
	}
	assertReconstructs(t, got, "You are a very helpful assistant.", "You are a helpful assistant.")
}

func TestTextDiffOf_TotalReplacement(t *testing.T) {
	got := TextDiffOf("alpha", "beta")
	if len(got.Segments) != 2 ||
		got.Segments[0].Kind != SegmentRemoved || got.Segments[0].Text != "alpha" ||
		got.Segments[1].Kind != SegmentAdded || got.Segments[1].Text != "beta" {
		t.Fatalf("unexpected segments: %+v", got.Segments)
	}
}

func TestTextDiffOf_EmptyToNonEmpty(t *testing.T) {
	got := TextDiffOf("", "hello world")
	if len(got.Segments) != 1 || got.Segments[0].Kind != SegmentAdded || got.Segments[0].Text != "hello world" {
		t.Fatalf("unexpected: %+v", got.Segments)
	}
}

func TestTextDiffOf_NonEmptyToEmpty(t *testing.T) {
	got := TextDiffOf("hello", "")
	if len(got.Segments) != 1 || got.Segments[0].Kind != SegmentRemoved || got.Segments[0].Text != "hello" {
		t.Fatalf("unexpected: %+v", got.Segments)
	}
}

// assertReconstructs verifies that filtering a TextDiff to (Equal +
// Removed) reproduces the before string and (Equal + Added) reproduces
// the after string. This is the diff's correctness invariant.
func assertReconstructs(t *testing.T, td *TextDiff, before, after string) {
	t.Helper()
	var bb, aa strings.Builder
	for _, seg := range td.Segments {
		switch seg.Kind {
		case SegmentEqual:
			bb.WriteString(seg.Text)
			aa.WriteString(seg.Text)
		case SegmentRemoved:
			bb.WriteString(seg.Text)
		case SegmentAdded:
			aa.WriteString(seg.Text)
		}
	}
	if bb.String() != before {
		t.Fatalf("Equal+Removed = %q, want %q (before)", bb.String(), before)
	}
	if aa.String() != after {
		t.Fatalf("Equal+Added = %q, want %q (after)", aa.String(), after)
	}
}
