package app

import (
	"ai-prov/internal/diff"
	"ai-prov/internal/snapshot"
	"ai-prov/internal/storage"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type StartRequest struct{ Task, Agent, Model string }
type StartResult struct {
	SessionID, SnapshotID, State, StartedAt string
	TrackedFiles, SkippedFiles              int
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
	Root         string
	MaxFileBytes int64
	Store        *storage.Store
}

func (s Service) Start(ctx context.Context, r StartRequest) (StartResult, error) {
	id, e := uuid()
	if e != nil {
		return StartResult{}, e
	}
	snap, e := snapshot.Create(s.Root, id, s.MaxFileBytes)
	if e != nil {
		return StartResult{}, e
	}
	startedAt := time.Now().UTC().Format(time.RFC3339)
	agent := r.Agent
	if agent == "" {
		agent = "unknown"
	}
	model := sql.NullString{}
	if r.Model != "" {
		model = sql.NullString{String: r.Model, Valid: true}
	}
	if e = s.Store.CreateSession(ctx, storage.Session{ID: id, ProjectPath: s.Root, Task: r.Task, Agent: agent, Model: model, State: "active", SnapshotID: id, StartedAt: startedAt}); e != nil {
		return StartResult{}, e
	}
	return StartResult{SessionID: id, SnapshotID: id, State: "active", StartedAt: startedAt, TrackedFiles: len(snap.Files), SkippedFiles: snap.SkippedCount}, nil
}
func (s Service) Finish(ctx context.Context, id string) (FinishResult, error) {
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
	changed, added, deleted := 0, 0, 0
	var events []storage.ChangeEvent
	var lines []storage.LineProvenance
	var changes []FileChange
	for _, f := range m.Files {
		before, e := os.ReadFile(filepath.Join(s.Root, ".ai-provenance", "snapshots", v.SnapshotID, filepath.FromSlash(f.Path)))
		if e != nil {
			_ = s.Store.FailSession(ctx, id, "SNAPSHOT_FAILED", e.Error())
			return FinishResult{}, fmt.Errorf("%w: %v", ErrSnapshotFailed, e)
		}
		after, _ := os.ReadFile(filepath.Join(s.Root, filepath.FromSlash(f.Path)))
		ed := diff.Diff(string(before), string(after))
		if diff.Hash(ed) != diff.Hash(nil) {
			conflict, err := s.Store.HasFinishedChange(ctx, f.Path, id)
			if err != nil {
				return FinishResult{}, err
			}
			if conflict {
				err = fmt.Errorf("%w: %s", ErrSessionBaselineConflict, f.Path)
				_ = s.Store.FailSession(ctx, id, "SESSION_BASELINE_CONFLICT", err.Error())
				return FinishResult{}, err
			}
			addedFile, deletedFile := diff.AddedNonBlank(ed), diff.DeletedNonBlank(ed)
			changed++
			added += addedFile
			deleted += deletedFile
			events = append(events, storage.ChangeEvent{ID: id + "-" + f.Path, SessionID: id, FilePath: f.Path, Status: "modified", AddedLines: addedFile, DeletedLines: deletedFile, DiffHash: diff.Hash(ed), CreatedAt: time.Now().UTC().Format(time.RFC3339)})
			lineCount := 0
			for n, e := range ed {
				if e.Op == diff.Insert {
					lines = append(lines, storage.LineProvenance{ID: fmt.Sprintf("%s-%s-%d", id, f.Path, n), FilePath: f.Path, LineIdentity: fmt.Sprintf("%x", e.Line), ContentHash: fmt.Sprintf("%x", e.Line), Source: "AI", OriginSessionID: sql.NullString{String: id, Valid: true}, CreatedAt: time.Now().UTC().Format(time.RFC3339)})
					lineCount++
				}
			}
			changes = append(changes, FileChange{Path: f.Path, Status: "modified", AddedLines: addedFile, DeletedLines: deletedFile, LineProvenanceCount: lineCount})
		}
	}
	if e = s.Store.CommitFinish(ctx, id, events, lines); e != nil {
		return FinishResult{}, e
	}
	finished, e := s.Store.GetSession(ctx, id)
	if e != nil {
		return FinishResult{}, e
	}
	return FinishResult{SessionID: id, State: "finished", FinishedAt: finished.FinishedAt.String, ChangedFiles: changed, AddedLines: added, DeletedLines: deleted, Changes: changes}, nil
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
