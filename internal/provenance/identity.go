package provenance

import (
	"crypto/sha256"
	"encoding/hex"
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
			out[i] = AI
		}
	}
	return out
}

func Identities(lines []string) []Identity {
	out := make([]Identity, len(lines))
	seen := map[string]int{}
	hs := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	for i, line := range lines {
		c := hs(line)
		seen[c]++
		before, after := "", ""
		if i > 0 {
			before = hs(lines[i-1])
		}
		if i+1 < len(lines) {
			after = hs(lines[i+1])
		}
		raw := c + "\x00" + string(rune(seen[c])) + "\x00" + before + "\x00" + after
		h := sha256.Sum256([]byte(raw))
		out[i] = Identity{c, seen[c], before, after, hex.EncodeToString(h[:])}
	}
	return out
}
