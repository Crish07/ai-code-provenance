// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

// Package diff calculates deterministic file and line differences.
package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

var ErrResourceLimit = errors.New("diff resource limit exceeded")

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

func DiffWithLimit(before, after string, maxEdits int) ([]Edit, error) {
	a, b := Lines(before), Lines(after)
	return myers(a, b, maxEdits)
}

// myers calculates a shortest edit script without allocating an n×m LCS
// matrix. Trace memory grows with edit distance, which keeps one large edited
// source file from exhausting memory merely because it has many lines.
func myers(a, b []string, maxEdits int) ([]Edit, error) {
	n, m := len(a), len(b)
	v := map[int]int{1: 0}
	trace := make([]map[int]int, 0, n+m+1)
	limit := n + m
	if maxEdits < limit {
		limit = maxEdits
	}
	for d := 0; d <= limit; d++ {
		next := make(map[int]int, 2*d+1)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1] < v[k+1]) {
				x = v[k+1]
			} else {
				x = v[k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			next[k] = x
			if x >= n && y >= m {
				trace = append(trace, next)
				return backtrack(a, b, trace), nil
			}
		}
		trace = append(trace, next)
		v = next
	}
	return nil, ErrResourceLimit
}

func backtrack(a, b []string, trace []map[int]int) []Edit {
	x, y := len(a), len(b)
	var reverse []Edit
	for d := len(trace) - 1; d > 0; d-- {
		v := trace[d-1]
		k := x - y
		prevK := k - 1
		if k == -d || (k != d && v[k-1] < v[k+1]) {
			prevK = k + 1
		}
		prevX, prevY := v[prevK], v[prevK]-prevK
		for x > prevX && y > prevY {
			reverse = append(reverse, Edit{Equal, a[x-1]})
			x--
			y--
		}
		if x == prevX {
			reverse = append(reverse, Edit{Insert, b[y-1]})
			y--
		} else {
			reverse = append(reverse, Edit{Delete, a[x-1]})
			x--
		}
	}
	for x > 0 && y > 0 {
		reverse = append(reverse, Edit{Equal, a[x-1]})
		x--
		y--
	}
	for x > 0 {
		reverse = append(reverse, Edit{Delete, a[x-1]})
		x--
	}
	for y > 0 {
		reverse = append(reverse, Edit{Insert, b[y-1]})
		y--
	}
	for i, j := 0, len(reverse)-1; i < j; i, j = i+1, j-1 {
		reverse[i], reverse[j] = reverse[j], reverse[i]
	}
	return reverse
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
