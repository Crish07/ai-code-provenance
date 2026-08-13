// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

// Package storage persists provenance data in project-local SQLite.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schemaVersion = 3

// IdentityVersionV2 marks rows whose content and identity hashes use the
// complete Line Identity format. Rows created by schema v1 cannot be upgraded
// losslessly because they lack occurrence and anchor data, so readers must not
// treat them as v2 provenance.
const IdentityVersionV2 = 2

var ErrLocked = errors.New("storage locked")

// Store owns a SQLite connection pool.
type Store struct{ db *sql.DB }

type Session struct {
	ID, ProjectPath, Task, Agent, AgentInstanceID, SnapshotID, State, StartedAt string
	Model, TaskKey, LastHeartbeatAt, FinishedAt, FailureCode, FailureMessage    sql.NullString
}
type FileSnapshot struct{ SnapshotID, FilePath, ContentHash, StoragePath string }
type ChangeEvent struct {
	ID, SessionID, FilePath, Status, DiffHash, CreatedAt string
	AddedLines, DeletedLines                             int
}
type LineProvenance struct {
	ID, FilePath, LineIdentity, ContentHash, Source, CreatedAt string
	OriginSessionID                                            sql.NullString
	IdentityVersion                                            int
}

// Open creates or opens a SQLite database and applies migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{"PRAGMA foreign_keys = ON", "PRAGMA journal_mode = WAL", "PRAGMA busy_timeout = 5000"} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("set sqlite pragma: %w", err)
		}
	}
	store := &Store{db: db}
	if err := store.Migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) SessionCounts(ctx context.Context) (active, finished, failed int, err error) {
	rows, err := s.db.QueryContext(ctx, "SELECT state, COUNT(*) FROM ai_session GROUP BY state")
	if err != nil {
		return 0, 0, 0, mapError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var count int
		if err = rows.Scan(&state, &count); err != nil {
			return
		}
		switch state {
		case "active":
			active = count
		case "finished":
			finished = count
		case "failed":
			failed = count
		}
	}
	err = rows.Err()
	return
}

func (s *Store) CreateSession(ctx context.Context, v Session) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO ai_session(id,project_path,task,agent,agent_instance_id,model,task_key,last_heartbeat_at,state,snapshot_id,started_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)", v.ID, v.ProjectPath, v.Task, v.Agent, v.AgentInstanceID, v.Model, v.TaskKey, v.LastHeartbeatAt, v.State, v.SnapshotID, v.StartedAt)
	return mapError(err)
}

func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	var v Session
	err := s.db.QueryRowContext(ctx, sessionSelect+" WHERE id=?", id).Scan(sessionScan(&v)...)
	return v, mapError(err)
}

// ActiveSessions returns active sessions for exactly one project in stable
// started_at/ID order. Callers use it for recovery diagnostics only.
func (s *Store) ActiveSessions(ctx context.Context, projectPath string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, sessionSelect+" WHERE project_path=? AND state='active' ORDER BY started_at, id", projectPath)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var v Session
		if err := rows.Scan(sessionScan(&v)...); err != nil {
			return nil, mapError(err)
		}
		out = append(out, v)
	}
	return out, mapError(rows.Err())
}

func (s *Store) ActiveSessionCount(ctx context.Context, projectPath, instance string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ai_session WHERE project_path=? AND agent_instance_id=? AND state='active'", projectPath, instance).Scan(&n)
	return n, mapError(err)
}

func (s *Store) Heartbeat(ctx context.Context, id, instance, at string) (bool, error) {
	result, err := s.db.ExecContext(ctx, "UPDATE ai_session SET last_heartbeat_at=? WHERE id=? AND agent_instance_id=? AND state='active'", at, id, instance)
	if err != nil {
		return false, mapError(err)
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *Store) ExpireLeases(ctx context.Context, projectPath, cutoff string) (int64, error) {
	result, err := s.db.ExecContext(ctx, "UPDATE ai_session SET state='failed',finished_at=?,failure_code='SESSION_LEASE_EXPIRED',failure_message='session heartbeat lease expired' WHERE project_path=? AND state='active' AND last_heartbeat_at<?", Now(), projectPath, cutoff)
	if err != nil {
		return 0, mapError(err)
	}
	return result.RowsAffected()
}

func (s *Store) LeaseExpiredSessionsBefore(ctx context.Context, projectPath, cutoff string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, sessionSelect+" WHERE project_path=? AND state='failed' AND failure_code='SESSION_LEASE_EXPIRED' AND finished_at IS NOT NULL AND finished_at<? ORDER BY finished_at,id", projectPath, cutoff)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var v Session
		if err := rows.Scan(sessionScan(&v)...); err != nil {
			return nil, mapError(err)
		}
		out = append(out, v)
	}
	return out, mapError(rows.Err())
}

// TerminalSessionsBefore returns only terminal sessions eligible for retention
// cleanup, ordered deterministically. Active sessions are intentionally absent.
func (s *Store) TerminalSessionsBefore(ctx context.Context, projectPath, cutoff string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, sessionSelect+" WHERE project_path=? AND state IN ('finished','failed') AND finished_at IS NOT NULL AND finished_at<? ORDER BY finished_at,id", projectPath, cutoff)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var v Session
		if err := rows.Scan(sessionScan(&v)...); err != nil {
			return nil, mapError(err)
		}
		out = append(out, v)
	}
	return out, mapError(rows.Err())
}

func (s *Store) UpdateSessionState(ctx context.Context, id, state string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE ai_session SET state=?, finished_at=? WHERE id=?", state, Now(), id)
	return mapError(err)
}

func (s *Store) SaveSnapshot(ctx context.Context, v FileSnapshot) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO file_snapshot(snapshot_id,file_path,content_hash,storage_path) VALUES (?,?,?,?)", v.SnapshotID, v.FilePath, v.ContentHash, v.StoragePath)
	return mapError(err)
}

func (s *Store) SaveChangeEvent(ctx context.Context, v ChangeEvent) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO ai_change_event(id,session_id,file_path,status,added_lines,deleted_lines,diff_hash,created_at) VALUES (?,?,?,?,?,?,?,?)", v.ID, v.SessionID, v.FilePath, v.Status, v.AddedLines, v.DeletedLines, v.DiffHash, v.CreatedAt)
	return mapError(err)
}

func (s *Store) SaveLineProvenance(ctx context.Context, v LineProvenance) error {
	if v.IdentityVersion == 0 {
		v.IdentityVersion = IdentityVersionV2
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO line_provenance(id,file_path,line_identity,content_hash,source,origin_session_id,created_at,identity_version) VALUES (?,?,?,?,?,?,?,?)", v.ID, v.FilePath, v.LineIdentity, v.ContentHash, v.Source, v.OriginSessionID, v.CreatedAt, v.IdentityVersion)
	return mapError(err)
}

func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// Migrate applies all schema changes transactionally and is idempotent.
func (s *Store) Migrate(ctx context.Context) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migration (version INTEGER PRIMARY KEY)"); err != nil {
			return err
		}
		var version int
		_ = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migration").Scan(&version)
		migrations := map[int][]string{
			1: {
				"CREATE TABLE ai_session (id TEXT PRIMARY KEY, project_path TEXT NOT NULL, task TEXT NOT NULL, agent TEXT NOT NULL DEFAULT 'unknown', model TEXT, state TEXT NOT NULL CHECK (state IN ('active','finished','failed')), snapshot_id TEXT NOT NULL, started_at TEXT NOT NULL, finished_at TEXT, failure_code TEXT, failure_message TEXT)",
				"CREATE TABLE file_snapshot (snapshot_id TEXT NOT NULL, file_path TEXT NOT NULL, content_hash TEXT NOT NULL, storage_path TEXT NOT NULL, PRIMARY KEY (snapshot_id, file_path))",
				"CREATE TABLE ai_change_event (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES ai_session(id), file_path TEXT NOT NULL, status TEXT NOT NULL, added_lines INTEGER NOT NULL, deleted_lines INTEGER NOT NULL, diff_hash TEXT NOT NULL, created_at TEXT NOT NULL)",
				"CREATE TABLE line_provenance (id TEXT PRIMARY KEY, file_path TEXT NOT NULL, line_identity TEXT NOT NULL, content_hash TEXT NOT NULL, source TEXT NOT NULL CHECK (source IN ('AI','Human','Unknown')), origin_session_id TEXT REFERENCES ai_session(id), created_at TEXT NOT NULL, removed_at TEXT)",
				"CREATE INDEX line_provenance_current_idx ON line_provenance(file_path, removed_at)",
			},
			2: {
				"ALTER TABLE line_provenance ADD COLUMN identity_version INTEGER NOT NULL DEFAULT 1",
				"CREATE UNIQUE INDEX line_provenance_current_v2_identity_idx ON line_provenance(file_path, line_identity) WHERE removed_at IS NULL AND identity_version = 2",
			},
			3: {
				"ALTER TABLE ai_session ADD COLUMN agent_instance_id TEXT NOT NULL DEFAULT 'legacy'",
				"ALTER TABLE ai_session ADD COLUMN task_key TEXT",
				"ALTER TABLE ai_session ADD COLUMN last_heartbeat_at TEXT",
				"UPDATE ai_session SET last_heartbeat_at=started_at WHERE last_heartbeat_at IS NULL",
				"CREATE INDEX ai_session_project_instance_active_idx ON ai_session(project_path,agent_instance_id,state,started_at)",
			},
		}
		for next := version + 1; next <= schemaVersion; next++ {
			for _, statement := range migrations[next] {
				if _, err := tx.ExecContext(ctx, statement); err != nil {
					return fmt.Errorf("apply schema v%d: %w", next, err)
				}
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migration(version) VALUES (?)", next); err != nil {
				return fmt.Errorf("record schema v%d: %w", next, err)
			}
		}
		return nil
	})
}

const sessionSelect = "SELECT id,project_path,task,agent,agent_instance_id,model,task_key,last_heartbeat_at,state,snapshot_id,started_at,finished_at,failure_code,failure_message FROM ai_session"

func sessionScan(v *Session) []any {
	return []any{&v.ID, &v.ProjectPath, &v.Task, &v.Agent, &v.AgentInstanceID, &v.Model, &v.TaskKey, &v.LastHeartbeatAt, &v.State, &v.SnapshotID, &v.StartedAt, &v.FinishedAt, &v.FailureCode, &v.FailureMessage}
}

// WithTx runs fn atomically.
func (s *Store) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return mapError(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return mapError(tx.Commit())
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) FinishAtomic(ctx context.Context, id string) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "UPDATE ai_session SET state='finished', finished_at=? WHERE id=? AND state='active'", Now(), id)
		return e
	})
}

// CommitFinish records a finish as one transaction. removedIDs are current v2
// rows superseded by an equal-line migration or invalidated by a deletion.
func (s *Store) CommitFinish(ctx context.Context, id string, events []ChangeEvent, lines []LineProvenance, removedIDs []string) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		for _, provenanceID := range removedIDs {
			if _, e := tx.ExecContext(ctx, "UPDATE line_provenance SET removed_at=? WHERE id=? AND removed_at IS NULL AND identity_version=?", Now(), provenanceID, IdentityVersionV2); e != nil {
				return e
			}
		}
		for _, v := range events {
			if _, e := tx.ExecContext(ctx, "INSERT INTO ai_change_event(id,session_id,file_path,status,added_lines,deleted_lines,diff_hash,created_at) VALUES (?,?,?,?,?,?,?,?)", v.ID, v.SessionID, v.FilePath, v.Status, v.AddedLines, v.DeletedLines, v.DiffHash, v.CreatedAt); e != nil {
				return e
			}
		}
		for _, v := range lines {
			if v.IdentityVersion == 0 {
				v.IdentityVersion = IdentityVersionV2
			}
			if _, e := tx.ExecContext(ctx, "INSERT INTO line_provenance(id,file_path,line_identity,content_hash,source,origin_session_id,created_at,identity_version) VALUES (?,?,?,?,?,?,?,?)", v.ID, v.FilePath, v.LineIdentity, v.ContentHash, v.Source, v.OriginSessionID, v.CreatedAt, v.IdentityVersion); e != nil {
				return e
			}
		}
		_, e := tx.ExecContext(ctx, "UPDATE ai_session SET state='finished',finished_at=? WHERE id=? AND state='active'", Now(), id)
		return e
	})
}

func (s *Store) FailSession(ctx context.Context, id, code, message string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE ai_session SET state='failed', finished_at=?, failure_code=?, failure_message=? WHERE id=? AND state='active'", Now(), code, message, id)
	return mapError(err)
}

// HasFinishedChangeSince reports whether another session finished a change to
// path after the current session began. Historical changes from before that
// baseline are intentionally irrelevant and must not create a conflict.
func (s *Store) HasFinishedChangeSince(ctx context.Context, path, except, startedAt string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ai_change_event e JOIN ai_session s ON s.id=e.session_id WHERE e.file_path=? AND e.session_id<>? AND s.state='finished' AND s.finished_at IS NOT NULL AND s.finished_at>?", path, except, startedAt).Scan(&n)
	return n > 0, mapError(err)
}

// CurrentAIByFile returns the unremoved AI provenance rows for a file. It is
// the read side used by the verifier to map git-added lines back to sessions.
func (s *Store) CurrentAIByFile(ctx context.Context, filePath string) ([]LineProvenance, error) {
	rows, err := s.currentByFile(ctx, filePath, true)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLineProvenance(rows)
}

// CurrentByFile returns all unremoved v2 provenance rows for filePath. Finish
// uses it to migrate all recorded sources, while verify uses CurrentAIByFile.
func (s *Store) CurrentByFile(ctx context.Context, filePath string) ([]LineProvenance, error) {
	rows, err := s.currentByFile(ctx, filePath, false)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	return scanLineProvenance(rows)
}

func (s *Store) currentByFile(ctx context.Context, filePath string, aiOnly bool) (*sql.Rows, error) {
	query := "SELECT id, file_path, line_identity, content_hash, source, origin_session_id, created_at, identity_version FROM line_provenance WHERE file_path=? AND removed_at IS NULL AND identity_version=?"
	if aiOnly {
		query += " AND source='AI'"
	}
	return s.db.QueryContext(ctx, query, filePath, IdentityVersionV2)
}

func scanLineProvenance(rows *sql.Rows) ([]LineProvenance, error) {
	var out []LineProvenance
	for rows.Next() {
		var v LineProvenance
		if err := rows.Scan(&v.ID, &v.FilePath, &v.LineIdentity, &v.ContentHash, &v.Source, &v.OriginSessionID, &v.CreatedAt, &v.IdentityVersion); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func mapError(err error) error {
	if err != nil && (contains(err.Error(), "locked") || contains(err.Error(), "busy")) {
		return fmt.Errorf("%w: %v", ErrLocked, err)
	}
	return err
}

func contains(s, part string) bool {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
