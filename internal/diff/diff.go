// Package diff calculates deterministic file and line differences.
package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

type Op string

const (
	Equal  Op = "equal"
	Insert Op = "insert"
	Delete Op = "delete"
)

type Edit struct {
	Op   Op
	Line string
}
type FileStatus string

const (
	Added     FileStatus = "added"
	Modified  FileStatus = "modified"
	Deleted   FileStatus = "deleted"
	Unchanged FileStatus = "unchanged"
)

func Classify(before, after *string) FileStatus {
	if before == nil {
		return Added
	}
	if after == nil {
		return Deleted
	}
	if *before == *after {
		return Unchanged
	}
	return Modified
}
func Lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
func Diff(before, after string) []Edit {
	a, b := Lines(before), Lines(after)
	n, m := len(a), len(b)
	d := make([][]int, n+1)
	for i := range d {
		d[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				d[i][j] = d[i+1][j+1] + 1
			} else if d[i+1][j] >= d[i][j+1] {
				d[i][j] = d[i+1][j]
			} else {
				d[i][j] = d[i][j+1]
			}
		}
	}
	var out []Edit
	for i, j := 0, 0; i < n || j < m; {
		if i < n && j < m && a[i] == b[j] {
			out = append(out, Edit{Equal, a[i]})
			i++
			j++
		} else if j < m && (i == n || d[i][j+1] > d[i+1][j]) {
			out = append(out, Edit{Insert, b[j]})
			j++
		} else {
			out = append(out, Edit{Delete, a[i]})
			i++
		}
	}
	return out
}
func AddedNonBlank(edits []Edit) int {
	n := 0
	for _, e := range edits {
		if e.Op == Insert && strings.TrimSpace(e.Line) != "" {
			n++
		}
	}
	return n
}
func DeletedNonBlank(edits []Edit) int {
	n := 0
	for _, e := range edits {
		if e.Op == Delete && strings.TrimSpace(e.Line) != "" {
			n++
		}
	}
	return n
}
func Hash(edits []Edit) string {
	h := sha256.New()
	for _, e := range edits {
		h.Write([]byte(e.Op))
		h.Write([]byte{0})
		h.Write([]byte(e.Line))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// HasChanges reports whether an edit script contains an insertion or deletion.
// Equal edits describe an unchanged file and must not be treated as a diff.
func HasChanges(edits []Edit) bool {
	for _, edit := range edits {
		if edit.Op != Equal {
			return true
		}
	}
	return false
}

func Renames(deleted, added map[string]string) map[string]string {
	out := map[string]string{}
	for old, h := range deleted {
		n := 0
		var next string
		for p, v := range added {
			if v == h {
				n++
				next = p
			}
		}
		if n == 1 {
			out[old] = next
		}
	}
	return out
}
