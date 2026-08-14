// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package diff

import "testing"

func TestDiff(t *testing.T) {
	e, err := DiffWithLimit("a\nb\n", "a\nc\n", 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(e) != 3 || e[0].Op != Equal || e[1].Op != Delete || e[2].Op != Insert {
		t.Fatalf("%#v", e)
	}
	if AddedNonBlank(e) != 1 {
		t.Fatal("count")
	}
}

func TestDiff_GoldenEdgeCases(t *testing.T) {
	tests := []struct {
		name, before, after string
		want                []Edit
	}{
		{"repeated lines", "x\nx\ny\n", "x\ny\nx\n", []Edit{{Equal, "x"}, {Delete, "x"}, {Equal, "y"}, {Insert, "x"}}},
		{"unicode", "你好\n世界\n", "你好\nCodex\n世界\n", []Edit{{Equal, "你好"}, {Insert, "Codex"}, {Equal, "世界"}}},
		{"canonicalized line endings", "a\nb\n", "a\nb\nc\n", []Edit{{Equal, "a"}, {Equal, "b"}, {Insert, "c"}}},
		{"ends", "middle\nend\n", "start\nmiddle\n", []Edit{{Insert, "start"}, {Equal, "middle"}, {Delete, "end"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DiffWithLimit(tt.before, tt.after, 16)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("Diff() = %#v, want %#v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("edit %d = %#v, want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestClassify(t *testing.T) {
	a, b := "a", "b"
	if Classify(nil, &a) != Added || Classify(&a, nil) != Deleted || Classify(&a, &a) != Unchanged || Classify(&a, &b) != Modified {
		t.Fatal("status")
	}
}

func TestDiff_EmptyDuplicateAndTrailingNewline(t *testing.T) {
	if got, err := DiffWithLimit("", "", 16); err != nil || len(got) != 0 {
		t.Fatal("empty")
	}
	e, err := DiffWithLimit("x\nx\n", "x\n", 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(e) != 2 || e[0].Op != Equal || e[1].Op != Delete {
		t.Fatalf("duplicate %#v", e)
	}
	if got, err := DiffWithLimit("a\n", "a", 16); err != nil || len(got) != 1 {
		t.Fatal("trailing newline")
	}
}

func TestHash(t *testing.T) {
	e, err := DiffWithLimit("a\n", "b\n", 16)
	if err != nil {
		t.Fatal(err)
	}
	if Hash(e) != Hash(e) {
		t.Fatal("unstable hash")
	}
}
