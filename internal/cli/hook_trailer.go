package cli

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"ai-prov/internal/provenance"
	"ai-prov/internal/storage"
)

// ourTrailerKeys is the set of Git trailer keys ai-prov owns. RewriteCommit
// Message removes prior occurrences of these keys before appending a fresh
// block, so amending a commit never stacks duplicate trailers.
var ourTrailerKeys = map[string]bool{
	"AI-Contribution":  true,
	"AI-Lines":         true,
	"AI-Agent":         true,
	"AI-Confidence":    true,
	"AI-Provenance-ID": true,
}

// RenderTrailer builds the ai-prov trailer block from a verify Result. It
// returns "" when there are no added lines to attribute, so the caller can
// skip appending an empty block. The format mirrors
// docs/AI-Code-Provenance-System-Development-Plan.md §14.
func RenderTrailer(res provenance.Result, agents string) string {
	if res.TotalAddedLines == 0 {
		return ""
	}
	if agents == "" {
		agents = "unknown"
	}
	pct := int(math.Round(res.Coverage * 100))
	var b strings.Builder
	fmt.Fprintf(&b, "AI-Contribution: %d%%\n", pct)
	fmt.Fprintf(&b, "AI-Lines: %d/%d\n", res.AIAddedLines, res.TotalAddedLines)
	fmt.Fprintf(&b, "AI-Agent: %s\n", agents)
	b.WriteString("AI-Confidence: 100%\n")
	if len(res.Sessions) > 0 {
		fmt.Fprintf(&b, "AI-Provenance-ID: %s\n", joinShortSessions(res.Sessions))
	}
	return b.String()
}

// RewriteCommitMessage replaces any prior ai-prov trailers in msg with the
// fresh trailer block, preserving the body and all foreign trailers (e.g.
// Signed-off-by, Reviewed-by). Prior occurrences of our keys are stripped from
// anywhere in the message so an amend never stacks duplicate blocks; runs of
// blank lines left behind by the removal are collapsed. When trailer is empty,
// prior ai-prov trailers are still stripped so amending a commit with
// write_trailer disabled does not leave stale records behind.
func RewriteCommitMessage(msg, trailer string) string {
	lines := strings.Split(msg, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if isOurTrailer(line) {
			continue
		}
		kept = append(kept, line)
	}
	cleaned := strings.Join(kept, "\n")
	for strings.Contains(cleaned, "\n\n\n") {
		cleaned = strings.ReplaceAll(cleaned, "\n\n\n", "\n\n")
	}
	cleaned = strings.TrimRight(cleaned, "\n")
	if trailer == "" {
		return cleaned + "\n"
	}
	return cleaned + "\n\n" + strings.TrimRight(trailer, "\n") + "\n"
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
