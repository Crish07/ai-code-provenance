// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package cli

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"ai-prov/internal/config"
	"ai-prov/internal/provenance"
	"ai-prov/internal/storage"
)

// ourTrailerKeys is the set of Git trailer keys ai-prov owns. RewriteCommit
// Message removes prior occurrences of these keys before appending a fresh
// block, so amending a commit never stacks duplicate trailers.
var ourTrailerKeys = map[string]bool{
	"AI-Contribution": true,
	"AI-Lines":        true,
	"AI-Agent":        true,
	// AI-Confidence was emitted by releases before H-001. Keep it owned only
	// for cleanup; it is no longer rendered because no confidence calculation
	// exists.
	"AI-Confidence":    true,
	"AI-Provenance-ID": true,
}

const trailerCommentMark = "# ai-prov trailer"

var titleCoverageSuffix = regexp.MustCompile(`\s*\[AI:[0-9]+%\]$`)

// RenderTrailer builds the ai-prov trailer block from a verify Result. It
// returns "" when there are no added lines to attribute, so the caller can
// skip appending an empty block. The format mirrors
// docs/AI-Code-Provenance-System-Development-Plan.md §14.
func RenderTrailer(res provenance.Result, agents string, settings config.HookConfig) string {
	if res.TotalAddedLines == 0 {
		return ""
	}
	if agents == "" {
		agents = "unknown"
	}
	pct := int(math.Round(res.Coverage * 100))
	var lines []string
	for _, field := range settings.Trailer.Fields {
		switch field {
		case "coverage":
			lines = append(lines, fmt.Sprintf("AI-Contribution: %d%%", pct))
		case "lines":
			lines = append(lines, fmt.Sprintf("AI-Lines: %d/%d", res.AIAddedLines, res.TotalAddedLines))
		case "agent":
			lines = append(lines, fmt.Sprintf("AI-Agent: %s", agents))
		case "provenance-id":
			if len(res.Sessions) > 0 {
				lines = append(lines, fmt.Sprintf("AI-Provenance-ID: %s", joinShortSessions(res.Sessions)))
			}
		}
	}
	if len(lines) == 0 {
		return ""
	}
	if settings.Trailer.Comments != nil && *settings.Trailer.Comments {
		return trailerCommentMark + "\n" + strings.Join(lines, "\n") + "\n"
	}
	return strings.Join(lines, "\n") + "\n"
}

// RewriteCommitMessage replaces any prior ai-prov trailers in msg with the
// fresh trailer block, preserving the body and all foreign trailers (e.g.
// Signed-off-by, Reviewed-by). Prior occurrences of our keys are stripped from
// anywhere in the message so an amend never stacks duplicate blocks; runs of
// blank lines left behind by the removal are collapsed. When trailer is empty,
// prior ai-prov trailers are still stripped so amending a commit with
// write_trailer disabled does not leave stale records behind.
func RewriteCommitMessage(msg, trailer string) string {
	return rewriteCommitMessage(msg, trailer, provenance.Result{}, false)
}

// RewriteCommitMessageWithCoverage applies the normal trailer replacement and
// additionally manages the ai-prov-owned coverage suffix on the first line.
// A zero-line diff removes an existing suffix instead of inventing a value.
func RewriteCommitMessageWithCoverage(msg, trailer string, res provenance.Result, titleCoverage bool) string {
	return rewriteCommitMessage(msg, trailer, res, titleCoverage)
}

func rewriteCommitMessage(msg, trailer string, res provenance.Result, titleCoverage bool) string {
	lines := strings.Split(msg, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if isOurTrailer(line) || line == trailerCommentMark {
			continue
		}
		kept = append(kept, line)
	}
	cleaned := strings.Join(kept, "\n")
	cleaned = rewriteTitleCoverage(cleaned, res, titleCoverage)
	for strings.Contains(cleaned, "\n\n\n") {
		cleaned = strings.ReplaceAll(cleaned, "\n\n\n", "\n\n")
	}
	cleaned = strings.TrimRight(cleaned, "\n")
	if trailer == "" {
		return cleaned + "\n"
	}
	return cleaned + "\n\n" + strings.TrimRight(trailer, "\n") + "\n"
}

func rewriteTitleCoverage(msg string, res provenance.Result, titleCoverage bool) string {
	lineEnd := strings.IndexByte(msg, '\n')
	if lineEnd < 0 {
		lineEnd = len(msg)
	}
	title := titleCoverageSuffix.ReplaceAllString(msg[:lineEnd], "")
	if titleCoverage && res.TotalAddedLines > 0 {
		title += fmt.Sprintf(" [AI:%d%%]", int(math.Round(res.Coverage*100)))
	}
	return title + msg[lineEnd:]
}

func isOurTrailer(line string) bool {
	colon := strings.IndexByte(line, ':')
	if colon <= 0 {
		return false
	}
	return ourTrailerKeys[line[:colon]]
}

// joinShortSessions joins session IDs, truncating each to 8 hex chars so the
// trailer stays compact when multiple sessions contributed to one commit.
func joinShortSessions(ids []string) string {
	out := make([]string, len(ids))
	for i, s := range ids {
		if len(s) > 8 {
			s = s[:8]
		}
		out[i] = s
	}
	return strings.Join(out, ",")
}

// sessionAgents returns the distinct agent labels of the contributing sessions
// in lexicographic order, joined by commas. Missing sessions are ignored; an
// empty agent is reported as "unknown". Returns "unknown" when ids is empty.
func sessionAgents(ctx context.Context, store *storage.Store, ids []string) string {
	if len(ids) == 0 {
		return "unknown"
	}
	seen := map[string]struct{}{}
	var agents []string
	for _, id := range ids {
		s, err := store.GetSession(ctx, id)
		if err != nil {
			continue
		}
		agent := s.Agent
		if agent == "" {
			agent = "unknown"
		}
		if _, ok := seen[agent]; ok {
			continue
		}
		seen[agent] = struct{}{}
		agents = append(agents, agent)
	}
	if len(agents) == 0 {
		return "unknown"
	}
	sort.Strings(agents)
	return strings.Join(agents, ",")
}
