package diff

import "testing"

func TestDiff(t *testing.T) {
	e := Diff("a\nb\n", "a\nc\n")
	if len(e) != 3 || e[0].Op != Equal || e[1].Op != Delete || e[2].Op != Insert {
		t.Fatalf("%#v", e)
	}
	if AddedNonBlank(e) != 1 {
		t.Fatal("count")
	}
}
func TestClassify(t *testing.T) {
	a, b := "a", "b"
	if Classify(nil, &a) != Added || Classify(&a, nil) != Deleted || Classify(&a, &a) != Unchanged || Classify(&a, &b) != Modified {
		t.Fatal("status")
	}
}
func TestDiff_EmptyDuplicateAndTrailingNewline(t *testing.T) {
	if len(Diff("", "")) != 0 {
		t.Fatal("empty")
	}
	e := Diff("x\nx\n", "x\n")
	if len(e) != 2 || e[0].Op != Equal || e[1].Op != Delete {
		t.Fatalf("duplicate %#v", e)
	}
	if len(Diff("a\n", "a")) != 1 {
		t.Fatal("trailing newline")
	}
}
func TestHashAndRenames(t *testing.T) {
	e := Diff("a\n", "b\n")
	if Hash(e) != Hash(e) {
		t.Fatal("unstable hash")
	}
	r := Renames(map[string]string{"old": "h"}, map[string]string{"new": "h", "x": "z"})
	if r["old"] != "new" {
		t.Fatal(r)
	}
	if len(Renames(map[string]string{"old": "h"}, map[string]string{"a": "h", "b": "h"})) != 0 {
		t.Fatal("ambiguous rename")
	}
}
