// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package app

import (
	"ai-prov/internal/diff"
	"ai-prov/internal/provenance"
	"ai-prov/internal/snapshot"
	"ai-prov/internal/storage"
	"ai-prov/internal/workspace"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"
)

type StartRequest struct{ Task, Agent, Model, AgentInstanceID, TaskKey string }
type StartResult struct {
	SessionID, SnapshotID, State, StartedAt, AgentInstanceID string
	TrackedFiles, SkippedFiles                               int
}

func bytesOrNil(v []byte) *string {
	if v == nil {
		return nil
	}
	s := string(v)
	return &s
}

func (s Service) fileCandidates(manifest snapshot.Manifest, current []workspace.File) ([]currentFile, error) {
	before := map[string][]byte{}
	for _, file := range manifest.Files {
		data, err := snapshot.ReadFile(s.Root, manifest, file)
		if err != nil {
			return nil, err
		}
		before[file.Path] = data
	}
	after := map[string][]byte{}
	for _, file := range current {
		after[file.Path] = snapshot.Normalize(file.Data)
	}
	paths := map[string]struct{}{}
	for path := range before {
		paths[path] = struct{}{}
	}
	for path := range after {
		paths[path] = struct{}{}
	}
	out := make([]currentFile, 0, len(paths))
	for path := range paths {
		b, a := before[path], after[path]
		if b == nil || !snapshot.Matches(a, snapshot.Hash(b)) {
			out = append(out, currentFile{path: path, before: b, after: a})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out, nil
}

type SessionStatusResult struct {
	SessionID      string
	State          string
	StartedAt      string
	FinishedAt     string
	FailureCode    string
	FailureMessage string
}
type ActiveSessionResult struct {
	SessionID, Task, Agent, AgentInstanceID, StartedAt, SnapshotID string
	TrackedFiles                                                   int
	SnapshotBytes                                                  int64
}
type SessionService interface {
	Start(context.Context, StartRequest) (StartResult, error)
}
type FileChange struct {
	Path                     string
	Status                   string
	AddedLines, DeletedLines int
	LineProvenanceCount      int
}
type FinishResult struct {
	SessionID, State, FinishedAt           string
	ChangedFiles, AddedLines, DeletedLines int
	Changes                                []FileChange
	Warnings                               []string
}
type Service struct {
	Root                       string
	MaxFileBytes               int64
	MaxSnapshotBytes           int64
	Store                      *storage.Store
	LeaseTimeout               time.Duration
	MaxActivePerAgentInstance  int
	ExpiredSessionGrace        time.Duration
	AutoReclaimExpiredSessions bool
}

type currentFile struct {
	index  int
	path   string
	before []byte
	after  []byte
	exists bool
}

type scannedFile struct {
	currentFile
	err error
}

func (s Service) Start(ctx context.Context, r StartRequest) (StartResult, error) {
	s.maintain(ctx)
	instance := r.AgentInstanceID
	var e error
	if instance == "" {
		instance, e = uuid()
		if e != nil {
			return StartResult{}, e
		}
	}
	if s.MaxActivePerAgentInstance > 0 {
		n, err := s.Store.ActiveSessionCount(ctx, s.Root, instance)
		if err != nil {
			return StartResult{}, err
		}
		if n >= s.MaxActivePerAgentInstance {
			return StartResult{}, ErrActiveSessionLimit
		}
	}
	id, e := uuid()
	if e != nil {
		return StartResult{}, e
	}
	snap, e := snapshot.CreateWithQuota(s.Root, id, s.MaxFileBytes, s.MaxSnapshotBytes)
	if e != nil {
		return StartResult{}, e
	}
	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	agent := r.Agent
	if agent == "" {
		agent = "unknown"
	}
	model := sql.NullString{}
	if r.Model != "" {
		model = sql.NullString{String: r.Model, Valid: true}
	}
	taskKey := sql.NullString{}
	if r.TaskKey != "" {
		taskKey = sql.NullString{String: r.TaskKey, Valid: true}
	}
	heartbeat := sql.NullString{String: startedAt, Valid: true}
	if e = s.Store.CreateSession(ctx, storage.Session{ID: id, ProjectPath: s.Root, Task: r.Task, Agent: agent, AgentInstanceID: instance, Model: model, TaskKey: taskKey, LastHeartbeatAt: heartbeat, State: "active", SnapshotID: id, StartedAt: startedAt}); e != nil {
		return StartResult{}, e
	}
	return StartResult{SessionID: id, SnapshotID: id, State: "active", StartedAt: startedAt, AgentInstanceID: instance, TrackedFiles: len(snap.Files), SkippedFiles: snap.SkippedCount}, nil
}

func (s Service) maintain(ctx context.Context) {
	if s.LeaseTimeout > 0 {
		_, _ = s.Store.ExpireLeases(ctx, s.Root, time.Now().UTC().Add(-s.LeaseTimeout).Format(time.RFC3339Nano))
	}
	if !s.AutoReclaimExpiredSessions || s.ExpiredSessionGrace <= 0 {
		return
	}
	sessions, err := s.Store.LeaseExpiredSessionsBefore(ctx, s.Root, time.Now().UTC().Add(-s.ExpiredSessionGrace).Format(time.RFC3339Nano))
	if err != nil {
		slog.Default().Warn("lease snapshot candidate query failed", "error", err)
		return
	}
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		ids = append(ids, session.SnapshotID)
	}
	if len(ids) == 0 {
		return
	}
	if _, err := snapshot.GCApply(s.Root, ids); err != nil {
		slog.Default().Warn("lease snapshot reclaim failed", "error", err)
	}
}

func (s Service) Heartbeat(ctx context.Context, id, instance string) (SessionStatusResult, error) {
	s.maintain(ctx)
	ok, err := s.Store.Heartbeat(ctx, id, instance, storage.Now())
	if err != nil {
		return SessionStatusResult{}, err
	}
	if !ok {
		return SessionStatusResult{}, ErrSessionOwnerMismatch
	}
	return s.Status(ctx, id)
}

// Status returns the persisted state of a session without reading the workspace,
// snapshot files, diff, or database content beyond the session row. It lets
// Agents confirm whether a timeout left a session as failed or still active
// before they decide whether to retry finish.
func (s Service) Status(ctx context.Context, id string) (SessionStatusResult, error) {
	s.maintain(ctx)
	v, err := s.Store.GetSession(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SessionStatusResult{}, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
		}
		return SessionStatusResult{}, err
	}
	out := SessionStatusResult{SessionID: v.ID, State: v.State, StartedAt: v.StartedAt}
	if v.FinishedAt.Valid {
		out.FinishedAt = v.FinishedAt.String
	}
	if v.FailureCode.Valid {
		out.FailureCode = v.FailureCode.String
	}
	if v.FailureMessage.Valid {
		out.FailureMessage = v.FailureMessage.String
	}
	return out, nil
}

// ActiveSessions exposes no source content. It is a recovery diagnostic for
// Agents that lost an in-memory session ID after Host context compaction.
func (s Service) ActiveSessions(ctx context.Context) ([]ActiveSessionResult, error) {
	sessions, err := s.Store.ActiveSessions(ctx, s.Root)
	if err != nil {
		return nil, err
	}
	out := make([]ActiveSessionResult, 0, len(sessions))
	for _, session := range sessions {
		manifest, err := snapshot.Read(s.Root, session.SnapshotID)
		if err != nil {
			return nil, fmt.Errorf("read active snapshot %s: %w", session.SnapshotID, err)
		}
		var size int64
		if manifest.Version >= 2 {
			for _, file := range manifest.Files {
				info, e := os.Stat(filepath.Join(s.Root, ".ai-provenance", "objects", file.Hash))
				if e != nil {
					return nil, fmt.Errorf("measure active object %s: %w", file.Hash, e)
				}
				size += info.Size()
			}
		} else {
			err = filepath.WalkDir(filepath.Join(s.Root, ".ai-provenance", "snapshots", session.SnapshotID), func(_ string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() {
					info, e := entry.Info()
					if e != nil {
						return e
					}
					size += info.Size()
				}
				return nil
			})
		}
		if err != nil {
			return nil, fmt.Errorf("measure active snapshot %s: %w", session.SnapshotID, err)
		}
		out = append(out, ActiveSessionResult{session.ID, session.Task, session.Agent, session.AgentInstanceID, session.StartedAt, session.SnapshotID, len(manifest.Files), size})
	}
	return out, nil
}

// Abandon explicitly terminates an active session that the caller has chosen
// not to finish. It does not delete its snapshot; later retention/GC owns
// reclaiming only terminal-session storage.
func (s Service) Abandon(ctx context.Context, id, reason string) (SessionStatusResult, error) {
	s.maintain(ctx)
	v, err := s.Store.GetSession(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && v.ProjectPath != s.Root) {
		return SessionStatusResult{}, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	if err != nil {
		return SessionStatusResult{}, err
	}
	if v.State != "active" {
		return SessionStatusResult{}, fmt.Errorf("%w: session %s in state %s", ErrSessionNotActive, id, v.State)
	}
	if err := s.Store.FailSession(ctx, id, "SESSION_ABANDONED", reason); err != nil {
		return SessionStatusResult{}, err
	}
	return s.Status(ctx, id)
}

func (s Service) AbandonOwned(ctx context.Context, id, instance, reason string) (SessionStatusResult, error) {
	v, err := s.Store.GetSession(ctx, id)
	if err != nil {
		return SessionStatusResult{}, err
	}
	if v.AgentInstanceID != instance {
		return SessionStatusResult{}, ErrSessionOwnerMismatch
	}
	return s.Abandon(ctx, id, reason)
}

func (s Service) FinishOwned(ctx context.Context, id, instance string) (FinishResult, error) {
	v, err := s.Store.GetSession(ctx, id)
	if err != nil {
		return FinishResult{}, err
	}
	if v.AgentInstanceID != instance {
		return FinishResult{}, ErrSessionOwnerMismatch
	}
	return s.Finish(ctx, id)
}

func (s Service) Finish(ctx context.Context, id string) (FinishResult, error) {
	s.maintain(ctx)
	started := time.Now()
	if err := ctx.Err(); err != nil {
		return FinishResult{}, s.interruptFinish(id, "session_load", 0, 0, 0, err)
	}
	v, e := s.Store.GetSession(ctx, id)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return FinishResult{}, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
		}
		return FinishResult{}, e
	}
	if v.State != "active" {
		return FinishResult{}, fmt.Errorf("%w: session %s in state %s", ErrSessionNotActive, id, v.State)
	}
	m, e := snapshot.Read(s.Root, v.SnapshotID)
	if e != nil {
		_ = s.Store.FailSession(ctx, id, "SNAPSHOT_FAILED", e.Error())
		return FinishResult{}, fmt.Errorf("%w: %v", ErrSnapshotFailed, e)
	}
	slog.Default().Info("session finish stage", "session_id", id, "stage", "manifest_load", "total_files", len(m.Files), "elapsed", time.Since(started))
	current, _, scanErr := workspace.Scan(s.Root, s.MaxFileBytes)
	if scanErr != nil {
		_ = s.Store.FailSession(ctx, id, "SNAPSHOT_FAILED", scanErr.Error())
		return FinishResult{}, fmt.Errorf("%w: %v", ErrSnapshotFailed, scanErr)
	}
	candidates, scanErr := s.fileCandidates(m, current)
	if scanErr != nil {
		_ = s.Store.FailSession(ctx, id, "SNAPSHOT_FAILED", scanErr.Error())
		return FinishResult{}, fmt.Errorf("%w: %v", ErrSnapshotFailed, scanErr)
	}
	scanned := len(current)

	changed, added, deleted := 0, 0, 0
	var events []storage.ChangeEvent
	var lines []storage.LineProvenance
	var removedProvenanceIDs []string
	var changes []FileChange
	for _, candidate := range candidates {
		slog.Default().Info("session finish stage", "session_id", id, "stage", "diff", "scanned_files", scanned, "total_files", len(m.Files), "candidate_files", len(changes)+1, "path", candidate.path, "bytes", len(candidate.after), "elapsed", time.Since(started))
		if err := ctx.Err(); err != nil {
			return FinishResult{}, s.interruptFinish(id, "diff", scanned, len(m.Files), len(changes), err)
		}
		ed, diffErr := diff.DiffWithLimit(string(candidate.before), string(candidate.after), 4096)
		if diffErr != nil {
			if errors.Is(diffErr, diff.ErrResourceLimit) {
				return FinishResult{}, s.failResourceLimit(id, &DiffResourceLimitError{Path: candidate.path, Bytes: len(candidate.after), Lines: len(diff.Lines(string(candidate.after)))})
			}
			return FinishResult{}, fmt.Errorf("%w: %v", ErrDiffFailed, diffErr)
		}
		if diff.HasChanges(ed) {
			conflict, err := s.Store.HasFinishedChangeSince(ctx, candidate.path, id, v.StartedAt)
			if err != nil {
				return FinishResult{}, err
			}
			if conflict {
				err = fmt.Errorf("%w: %s", ErrSessionBaselineConflict, candidate.path)
				_ = s.Store.FailSession(ctx, id, "SESSION_BASELINE_CONFLICT", err.Error())
				return FinishResult{}, err
			}
			addedFile, deletedFile := diff.AddedNonBlank(ed), diff.DeletedNonBlank(ed)
			changed++
			added += addedFile
			deleted += deletedFile
			status := diff.Classify(bytesOrNil(candidate.before), bytesOrNil(candidate.after))
			events = append(events, storage.ChangeEvent{ID: id + "-" + candidate.path, SessionID: id, FilePath: candidate.path, Status: string(status), AddedLines: addedFile, DeletedLines: deletedFile, DiffHash: diff.Hash(ed), CreatedAt: time.Now().UTC().Format(time.RFC3339)})
			migrated, removed, lineCount, err := s.provenanceForEdits(ctx, id, candidate.path, string(candidate.before), string(candidate.after), ed)
			if err != nil {
				return FinishResult{}, err
			}
			lines = append(lines, migrated...)
			removedProvenanceIDs = append(removedProvenanceIDs, removed...)
			changes = append(changes, FileChange{Path: candidate.path, Status: string(status), AddedLines: addedFile, DeletedLines: deletedFile, LineProvenanceCount: lineCount})
		}
	}
	if err := ctx.Err(); err != nil {
		return FinishResult{}, s.interruptFinish(id, "storage_commit", len(m.Files), len(m.Files), len(changes), err)
	}
	slog.Default().Info("session finish stage", "session_id", id, "stage", "storage_commit", "scanned_files", scanned, "candidate_files", len(changes), "elapsed", time.Since(started))
	if e = s.Store.CommitFinish(ctx, id, events, lines, removedProvenanceIDs); e != nil {
		return FinishResult{}, e
	}
	finished, e := s.Store.GetSession(ctx, id)
	if e != nil {
		return FinishResult{}, e
	}
	slog.Default().Info("session finish completed", "session_id", id, "scanned_files", scanned, "changed_files", changed, "elapsed", time.Since(started))
	return FinishResult{SessionID: id, State: "finished", FinishedAt: finished.FinishedAt.String, ChangedFiles: changed, AddedLines: added, DeletedLines: deleted, Changes: changes}, nil
}

// provenanceForEdits reconstructs identities for the baseline and current
// file, carries a recorded source across equal edits, and invalidates every
// current row not carried. The edit script gives a deterministic one-to-one
// mapping for duplicate lines; unrecorded equal lines remain unrecorded.
func (s Service) provenanceForEdits(ctx context.Context, sessionID, filePath, before, after string, edits []diff.Edit) ([]storage.LineProvenance, []string, int, error) {
	previous, err := s.Store.CurrentByFile(ctx, filePath)
	if err != nil {
		return nil, nil, 0, err
	}
	beforeIDs := provenance.Identities(filePath, diff.Lines(before))
	afterIDs := provenance.Identities(filePath, diff.Lines(after))
	byIdentity := make(map[string]storage.LineProvenance, len(previous))
	for _, row := range previous {
		byIdentity[row.LineIdentity] = row
	}

	lines := make([]storage.LineProvenance, 0)
	beforeIndex, afterIndex, inserted := 0, 0, 0
	for editIndex, edit := range edits {
		switch edit.Op {
		case diff.Equal:
			if beforeIndex >= len(beforeIDs) || afterIndex >= len(afterIDs) {
				return nil, nil, 0, fmt.Errorf("invalid equal edit positions for %s", filePath)
			}
			if old, ok := byIdentity[beforeIDs[beforeIndex].Hash]; ok {
				lines = append(lines, storage.LineProvenance{ID: fmt.Sprintf("%s-%s-equal-%d", sessionID, filePath, editIndex), FilePath: filePath, LineIdentity: afterIDs[afterIndex].Hash, ContentHash: afterIDs[afterIndex].ContentHash, Source: old.Source, OriginSessionID: old.OriginSessionID, CreatedAt: time.Now().UTC().Format(time.RFC3339), IdentityVersion: storage.IdentityVersionV2})
			}
			beforeIndex++
			afterIndex++
		case diff.Insert:
			if afterIndex >= len(afterIDs) {
				return nil, nil, 0, fmt.Errorf("invalid insert edit position for %s", filePath)
			}
			identity := afterIDs[afterIndex]
			lines = append(lines, storage.LineProvenance{ID: fmt.Sprintf("%s-%s-insert-%d", sessionID, filePath, editIndex), FilePath: filePath, LineIdentity: identity.Hash, ContentHash: identity.ContentHash, Source: "AI", OriginSessionID: sql.NullString{String: sessionID, Valid: true}, CreatedAt: time.Now().UTC().Format(time.RFC3339), IdentityVersion: storage.IdentityVersionV2})
			inserted++
			afterIndex++
		case diff.Delete:
			if beforeIndex >= len(beforeIDs) {
				return nil, nil, 0, fmt.Errorf("invalid delete edit position for %s", filePath)
			}
			beforeIndex++
		}
	}
	if beforeIndex != len(beforeIDs) || afterIndex != len(afterIDs) {
		return nil, nil, 0, fmt.Errorf("incomplete edit positions for %s", filePath)
	}
	// A changed file receives a newly reconstructed current identity set. Mark
	// every prior v2 row removed first, including rows carried through equal
	// edits: an unchanged identity can otherwise collide with the partial
	// unique index before its replacement is inserted.
	removed := make([]string, 0, len(previous))
	for _, row := range previous {
		removed = append(removed, row.ID)
	}
	return lines, removed, inserted, nil
}

// scanCurrentFiles reads and hashes every manifest file independently of
// Agent-reported paths. It retains bytes only for changed/deleted candidates,
// then orders them by manifest position for deterministic later processing.
func (s Service) scanCurrentFiles(ctx context.Context, files []snapshot.File) ([]currentFile, int, error) {
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	jobs := make(chan int)
	results := make(chan scannedFile, workers)
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-workCtx.Done():
					return
				case index, ok := <-jobs:
					if !ok {
						return
					}
					file := files[index]
					after, err := os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(file.Path)))
					exists := err == nil
					if err != nil && !errors.Is(err, os.ErrNotExist) {
						select {
						case results <- scannedFile{currentFile: currentFile{index: index}, err: err}:
						case <-workCtx.Done():
						}
						return
					}
					after = snapshot.Normalize(after)
					result := scannedFile{currentFile: currentFile{index: index, exists: exists}}
					if !exists || !snapshot.Matches(after, file.Hash) {
						result.after = after
					}
					select {
					case results <- result:
					case <-workCtx.Done():
						return
					}
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range files {
			select {
			case jobs <- index:
			case <-workCtx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	candidates := make([]currentFile, 0)
	scanned := 0
	var firstErr error
	for result := range results {
		scanned++
		if result.err != nil && firstErr == nil {
			firstErr = result.err
			cancel()
			continue
		}
		if result.after != nil || !result.exists {
			candidates = append(candidates, result.currentFile)
		}
	}
	if firstErr != nil {
		return candidates, scanned, firstErr
	}
	if err := ctx.Err(); err != nil {
		return candidates, scanned, err
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].index < candidates[j].index })
	return candidates, scanned, nil
}

func (s Service) interruptFinish(id, stage string, scanned, total, candidates int, cause error) error {
	code := "FINISH_CANCELLED"
	sentinel := ErrFinishCancelled
	if errors.Is(cause, context.DeadlineExceeded) {
		code = "FINISH_TIMEOUT"
		sentinel = ErrFinishTimeout
	}
	interrupted := &FinishInterruptedError{Cause: sentinel, Stage: stage, Scanned: scanned, Total: total, Candidates: candidates}
	failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Store.FailSession(failureCtx, id, code, interrupted.Error()); err != nil {
		slog.Default().Error("session finish cancellation could not be recorded", "session_id", id, "stage", stage, "error", err)
	}
	slog.Default().Warn("session finish interrupted", "session_id", id, "stage", stage, "scanned_files", scanned, "total_files", total, "candidate_files", candidates, "reason", code)
	return interrupted
}

// failResourceLimit records a terminal, non-retryable finish failure using an
// independent context. The caller's context may be near a host deadline, but a
// resource-limited session must never remain active or acquire partial
// provenance that a later finish could misattribute.
func (s Service) failResourceLimit(id string, limit *DiffResourceLimitError) error {
	failureCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Store.FailSession(failureCtx, id, "DIFF_RESOURCE_LIMIT", limit.Error()); err != nil {
		slog.Default().Error("session finish resource limit could not be recorded", "session_id", id, "path", limit.Path, "error", err)
	}
	slog.Default().Warn("session finish resource limit", "session_id", id, "path", limit.Path, "bytes", limit.Bytes, "lines", limit.Lines)
	return limit
}

func uuid() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
