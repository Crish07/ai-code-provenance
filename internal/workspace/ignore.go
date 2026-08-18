// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package workspace

import (
	"fmt"
	"path"
	"strings"
)

// DefaultIgnoreRules seeds each new project's ai-prov-specific ignore file.
// The scanner treats these as ordinary rules after init, so users can extend
// them for generated content in their own project. .git and .ai-provenance
// remain protected separately even if a user removes their visible entries.
const DefaultIgnoreRules = `# ai-prov workspace ignore rules
# Add project-local generated files or directories below.
.git/
.ai-provenance/
.agents/
.claude/
.codex/
.cursor/
.trae/
.gitnexus/
node_modules/
vendor/
dist/
build/
`

// ignoreMatcher implements the project-root subset of Git's ignore syntax
// used by workspace scanning. Rules are line-oriented and later matching rules
// win. It supports comments, ! negation, *, ?, [], **, root-relative paths,
// and directory rules ending in /. Nested ignore files, escaped trailing
// spaces, and Git attributes are deliberately outside this local scanner.
type ignoreMatcher struct {
	rules      []ignoreRule
	hasNegated bool
}

type ignoreRule struct {
	pattern   string
	directory bool
	negated   bool
	hasSlash  bool
	rooted    bool
}

func parseIgnore(contents string) (ignoreMatcher, error) {
	var out ignoreMatcher
	for lineNumber, raw := range strings.Split(contents, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			rule.negated = true
			line = line[1:]
			out.hasNegated = true
		}
		if line == "" {
			return ignoreMatcher{}, fmt.Errorf("ignore rule %d has an empty pattern", lineNumber+1)
		}
		if strings.HasPrefix(line, "/") {
			rule.rooted = true
			line = strings.TrimPrefix(line, "/")
		}
		rule.directory = strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		if line == "" {
			return ignoreMatcher{}, fmt.Errorf("ignore rule %d has an empty path", lineNumber+1)
		}
		line = strings.ReplaceAll(line, "\\", "/")
		rule.pattern = line
		rule.hasSlash = strings.Contains(line, "/")
		for _, segment := range strings.Split(line, "/") {
			if segment == "**" {
				continue
			}
			if _, err := path.Match(segment, ""); err != nil {
				return ignoreMatcher{}, fmt.Errorf("ignore rule %d: %w", lineNumber+1, err)
			}
		}
		out.rules = append(out.rules, rule)
	}
	return out, nil
}

func (m ignoreMatcher) ignored(rel string, isDir bool) bool {
	parts := strings.Split(rel, "/")
	ignored := false
	for _, rule := range m.rules {
		if rule.matches(parts, isDir) {
			ignored = !rule.negated
		}
	}
	return ignored
}

// mayIncludeDescendant prevents directory pruning from making a later ! rule
// unreachable. Keeping the directory walk only for the relevant subtree
// preserves negation semantics without disabling pruning for unrelated trees.
func (m ignoreMatcher) mayIncludeDescendant(rel string) bool {
	if !m.hasNegated {
		return false
	}
	prefix := strings.Split(rel, "/")
	for _, rule := range m.rules {
		if !rule.negated {
			continue
		}
		if !rule.hasSlash || ignorePatternMayHavePrefix(strings.Split(rule.pattern, "/"), prefix) {
			return true
		}
	}
	return false
}

func ignorePatternMayHavePrefix(pattern, prefix []string) bool {
	if len(prefix) == 0 {
		return true
	}
	if len(pattern) == 0 {
		return false
	}
	if pattern[0] == "**" {
		return ignorePatternMayHavePrefix(pattern[1:], prefix) || ignorePatternMayHavePrefix(pattern, prefix[1:])
	}
	ok, _ := path.Match(pattern[0], prefix[0])
	return ok && ignorePatternMayHavePrefix(pattern[1:], prefix[1:])
}

func (r ignoreRule) matches(parts []string, isDir bool) bool {
	if !r.directory {
		if !r.hasSlash {
			if r.rooted && len(parts) != 1 {
				return false
			}
			ok, _ := path.Match(r.pattern, parts[len(parts)-1])
			return ok
		}
		return matchIgnorePath(strings.Split(r.pattern, "/"), parts)
	}
	max := len(parts)
	if !isDir {
		max--
	}
	for end := 1; end <= max; end++ {
		candidate := parts[:end]
		if !r.hasSlash {
			if r.rooted {
				ok, _ := path.Match(r.pattern, parts[0])
				return end == 1 && ok
			}
			ok, _ := path.Match(r.pattern, candidate[len(candidate)-1])
			if ok {
				return true
			}
			continue
		}
		if matchIgnorePath(strings.Split(r.pattern, "/"), candidate) {
			return true
		}
	}
	return false
}

func matchIgnorePath(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	if pattern[0] == "**" {
		for offset := 0; offset <= len(name); offset++ {
			if matchIgnorePath(pattern[1:], name[offset:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, _ := path.Match(pattern[0], name[0])
	return ok && matchIgnorePath(pattern[1:], name[1:])
}
