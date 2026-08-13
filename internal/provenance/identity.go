// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package provenance

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

type Identity struct {
	ContentHash         string
	Occurrence          int
	Before, After, Hash string
}
type Source string

const (
	AI      Source = "AI"
	Unknown Source = "Unknown"
)

type Migration struct {
	Sources []Source
	Removed []int
}

// ContentHash returns the SHA-256 hash of a line after normalizing a possible
// CRLF line ending. It is shared by persistence and the verifier's temporary
// content-level matching.
func ContentHash(line string) string {
	line = strings.TrimSuffix(line, "\r")
	h := sha256.Sum256([]byte(line))
	return hex.EncodeToString(h[:])
}

func MigrateWithRemovals(before, after []string, existing []Source) Migration {
	sources := Migrate(before, after, existing)
	used := make([]bool, len(before))
	for _, line := range after {
		for j, old := range before {
			if !used[j] && line == old {
				used[j] = true
				break
			}
		}
	}
	var removed []int
	for i := range before {
		if !used[i] {
			removed = append(removed, i)
		}
	}
	return Migration{sources, removed}
}

func Migrate(before, after []string, existing []Source) []Source {
	out := make([]Source, len(after))
	used := make([]bool, len(before))
	for i, line := range after {
		for j, old := range before {
			if !used[j] && line == old && j < len(existing) {
				out[i] = existing[j]
				used[j] = true
				break
			}
		}
		if out[i] == "" {
			// A line that cannot be matched exactly to an existing baseline
			// line is not proven to be AI-generated. In particular, this
			// covers human rewrites and newly introduced unrecorded lines.
			out[i] = Unknown
		}
	}
	return out
}

// Identities returns deterministic identities for normalized lines in filePath.
// A line's occurrence distinguishes duplicate content. Its anchors are the
// nearest non-blank neighbors, allowing a matching line to retain a stable
// context across blank-line-only edits.
func Identities(filePath string, lines []string) []Identity {
	out := make([]Identity, len(lines))
	seen := map[string]int{}
	normalized := make([]string, len(lines))
	for i, line := range lines {
		// Callers normally receive LF-normalized content from snapshot, but
		// identity construction itself must not make CRLF and LF differ.
		normalized[i] = strings.TrimSuffix(line, "\r")
	}
	for i, line := range lines {
		line = normalized[i]
		c := ContentHash(line)
		seen[c]++
		before, after := "", ""
		for j := i - 1; j >= 0; j-- {
			if strings.TrimSpace(normalized[j]) != "" {
				before = ContentHash(normalized[j])
				break
			}
		}
		for j := i + 1; j < len(normalized); j++ {
			if strings.TrimSpace(normalized[j]) != "" {
				after = ContentHash(normalized[j])
				break
			}
		}
		raw := filePath + "\x00" + c + "\x00" + strconv.Itoa(seen[c]) + "\x00" + before + "\x00" + after
		h := sha256.Sum256([]byte(raw))
		out[i] = Identity{c, seen[c], before, after, hex.EncodeToString(h[:])}
	}
	return out
}
