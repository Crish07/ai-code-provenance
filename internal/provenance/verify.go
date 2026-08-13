// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package provenance

import (
	"context"
	"sort"

	"ai-prov/internal/git"
	"ai-prov/internal/storage"
)

// Status mirrors docs/MCP-Tool-API-Specification.md §6 verify.status.
type Status string

const (
	// StatusOK means every added effective line is covered by AI provenance, or
	// there are no added effective lines at all.
	StatusOK Status = "ok"
	// StatusWarning means some added lines are uncovered and strict=false.
	StatusWarning Status = "warning"
	// StatusFailed means some added lines are uncovered and strict=true.
	StatusFailed Status = "failed"
)

// LineSource classifies a single added line against AI provenance.
type LineSource string

const (
	// LineSourceAI marks a line matched to an unremoved AI provenance row.
	LineSourceAI LineSource = "AI"
	// LineSourceUnknown marks a line with no matching AI provenance.
	LineSourceUnknown LineSource = "unknown"
)

// Request is the input to Verifier.Verify.
type Request struct {
	Scope  git.Scope
	Strict bool
}

// LineReport describes a single added effective line in the diff, with its
// provenance classification and the originating session when known.
type LineReport struct {
	Content   string
	Source    LineSource
	SessionID string
}

// FileReport groups the per-line reports for a single file in the diff.
type FileReport struct {
	Path       string
	AddedLines []LineReport
}

// Result mirrors the provenance.verify success response schema. Files carries
// the per-line breakdown used by the CLI report; the MCP tool omits it to
// honor additionalProperties:false.
type Result struct {
	Status              Status
	Scope               git.Scope
	TotalAddedLines     int
	AIAddedLines        int
	UntrackedAddedLines int
	Coverage            float64
	Sessions            []string
	UncoveredFiles      []string
	Files               []FileReport
}

// GitDiffReader reads the git diff for a scope. *git.Reader satisfies it.
type GitDiffReader interface {
	ReadDiff(ctx context.Context, scope git.Scope) ([]git.FileDiff, error)
}

// AIProvenanceStore reads unremoved AI provenance rows. *storage.Store
// satisfies it via CurrentAIByFile.
type AIProvenanceStore interface {
	CurrentAIByFile(ctx context.Context, filePath string) ([]storage.LineProvenance, error)
}

// Verifier maps git-added effective lines onto current AI provenance and
// computes coverage, following docs/Provenance-Engine-Design.md §7. It is
// the shared engine used by both the CLI and the MCP tool.
type Verifier struct {
	Git   GitDiffReader
	Store AIProvenanceStore
}

// Verify reads the diff for req.Scope and matches each added non-blank line
// against unremoved v2 AI provenance by file_path + SHA-256 content_hash.
// Git's current diff reader has no hunk positions or full worktree content, so
// this is deliberately a bounded content-level fallback, not full identity
// matching; duplicate coverage is capped by the number of provenance rows.
func (v Verifier) Verify(ctx context.Context, req Request) (Result, error) {
	diffs, err := v.Git.ReadDiff(ctx, req.Scope)
	if err != nil {
		return Result{}, err
	}
	res := Result{
		Scope:          req.Scope,
		Status:         StatusOK,
		Coverage:       1,
		Sessions:       []string{},
		UncoveredFiles: []string{},
		Files:          []FileReport{},
	}
	for _, fd := range diffs {
		// Group added lines by content_hash so duplicates are matched
		// against at most the same number of AI rows.
		type counter struct{ diff, ai int }
		counts := map[string]*counter{}
		for _, line := range fd.AddedLines {
			h := ContentHash(line)
			if c, ok := counts[h]; ok {
				c.diff++
			} else {
				counts[h] = &counter{diff: 1}
			}
		}

		rows, err := v.Store.CurrentAIByFile(ctx, fd.Path)
		if err != nil {
			return Result{}, err
		}
		// CurrentAIByFile does not impose an order; sort by ID so per-line
		// session attribution is deterministic for identical content hashes.
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })

		// Each AI row can cover at most one occurrence of its content hash in
		// the diff. Queue contributing sessions per hash so the per-line
		// report can attribute them in first-seen order, and track unique
		// contributing sessions so callers can attribute work.
		sessionsByHash := map[string][]string{}
		sessions := map[string]struct{}{}
		for _, row := range rows {
			c, ok := counts[row.ContentHash]
			if !ok || c.ai >= c.diff {
				continue
			}
			c.ai++
			sid := ""
			if row.OriginSessionID.Valid {
				sid = row.OriginSessionID.String
				sessions[sid] = struct{}{}
			}
			sessionsByHash[row.ContentHash] = append(sessionsByHash[row.ContentHash], sid)
		}

		// Build per-line reports in diff order, popping queued sessions so
		// duplicate lines exhaust AI coverage deterministically.
		lines := make([]LineReport, 0, len(fd.AddedLines))
		fileAI, fileUntracked := 0, 0
		for _, line := range fd.AddedLines {
			h := ContentHash(line)
			queue := sessionsByHash[h]
			lr := LineReport{Content: line, Source: LineSourceUnknown}
			if len(queue) > 0 {
				lr.Source = LineSourceAI
				lr.SessionID = queue[0]
				sessionsByHash[h] = queue[1:]
				fileAI++
			} else {
				fileUntracked++
			}
			lines = append(lines, lr)
		}

		res.TotalAddedLines += len(fd.AddedLines)
		res.AIAddedLines += fileAI
		res.UntrackedAddedLines += fileUntracked
		for sid := range sessions {
			res.Sessions = append(res.Sessions, sid)
		}
		if fileUntracked > 0 {
			res.UncoveredFiles = append(res.UncoveredFiles, fd.Path)
		}
		res.Files = append(res.Files, FileReport{Path: fd.Path, AddedLines: lines})
	}

	if res.TotalAddedLines > 0 {
		res.Coverage = float64(res.AIAddedLines) / float64(res.TotalAddedLines)
		switch {
		case res.UntrackedAddedLines == 0:
			res.Status = StatusOK
			res.Coverage = 1
		case req.Strict:
			res.Status = StatusFailed
		default:
			res.Status = StatusWarning
		}
	}

	sort.Strings(res.Sessions)
	sort.Strings(res.UncoveredFiles)
	return res, nil
}
