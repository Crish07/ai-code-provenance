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

const schemaVersion = 1

var ErrLocked = errors.New("storage locked")

// Store owns a SQLite connection pool.
type Store struct{ db *sql.DB }

type Session struct {
	ID, ProjectPath, Task, Agent, SnapshotID, State, StartedAt string
	Model, FinishedAt, FailureCode, FailureMessage             sql.NullString
}
type FileSnapshot struct{ SnapshotID, FilePath, ContentHash, StoragePath string }
type ChangeEvent struct {
	ID, SessionID, FilePath, Status, DiffHash, CreatedAt string
	AddedLines, DeletedLines                             int
}
type LineProvenance struct {
	ID, FilePath, LineIdentity, ContentHash, Source, CreatedAt string
	OriginSessionID                                            sql.NullString
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
	_, err := s.db.ExecContext(ctx, "INSERT INTO ai_session(id,project_path,task,agent,model,state,snapshot_id,started_at) VALUES (?,?,?,?,?,?,?,?)", v.ID, v.ProjectPath, v.Task, v.Agent, v.Model, v.State, v.SnapshotID, v.StartedAt)
	return mapError(err)
}
func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	var v Session
	err := s.db.QueryRowContext(ctx, "SELECT id,project_path,task,agent,model,state,snapshot_id,started_at,finished_at,failure_code,failure_message FROM ai_session WHERE id=?", id).Scan(&v.ID, &v.ProjectPath, &v.Task, &v.Agent, &v.Model, &v.State, &v.SnapshotID, &v.StartedAt, &v.FinishedAt, &v.FailureCode, &v.FailureMessage)
	return v, mapError(err)
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
	_, err := s.db.ExecContext(ctx, "INSERT INTO line_provenance(id,file_path,line_identity,content_hash,source,origin_session_id,created_at) VALUES (?,?,?,?,?,?,?)", v.ID, v.FilePath, v.LineIdentity, v.ContentHash, v.Source, v.OriginSessionID, v.CreatedAt)
	return mapError(err)
}
func Now() string { return time.Now().UTC().Format(time.RFC3339) }

// Migrate applies all schema changes transactionally and is idempotent.
func (s *Store) Migrate(ctx context.Context) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migration (version INTEGER PRIMARY KEY)"); err != nil {
			return err
		}
		var version int
		_ = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migration").Scan(&version)
		if version >= schemaVersion {
			return nil
		}
		for _, statement := range []string{
			"CREATE TABLE ai_session (id TEXT PRIMARY KEY, project_path TEXT NOT NULL, task TEXT NOT NULL, agent TEXT NOT NULL DEFAULT 'unknown', model TEXT, state TEXT NOT NULL CHECK (state IN ('active','finished','failed')), snapshot_id TEXT NOT NULL, started_at TEXT NOT NULL, finished_at TEXT, failure_code TEXT, failure_message TEXT)",
			"CREATE TABLE file_snapshot (snapshot_id TEXT NOT NULL, file_path TEXT NOT NULL, content_hash TEXT NOT NULL, storage_path TEXT NOT NULL, PRIMARY KEY (snapshot_id, file_path))",
			"CREATE TABLE ai_change_event (id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES ai_session(id), file_path TEXT NOT NULL, status TEXT NOT NULL, added_lines INTEGER NOT NULL, deleted_lines INTEGER NOT NULL, diff_hash TEXT NOT NULL, created_at TEXT NOT NULL)",
			"CREATE TABLE line_provenance (id TEXT PRIMARY KEY, file_path TEXT NOT NULL, line_identity TEXT NOT NULL, content_hash TEXT NOT NULL, source TEXT NOT NULL CHECK (source IN ('AI','Human','Unknown')), origin_session_id TEXT REFERENCES ai_session(id), created_at TEXT NOT NULL, removed_at TEXT)",
			"CREATE INDEX line_provenance_current_idx ON line_provenance(file_path, removed_at)",
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply schema: %w", err)
			}
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO schema_migration(version) VALUES (?)", schemaVersion)
		return err
	})
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
func (s *Store) CommitFinish(ctx context.Context, id string, events []ChangeEvent, lines []LineProvenance) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		for _, v := range events {
			if _, e := tx.ExecContext(ctx, "INSERT INTO ai_change_event(id,session_id,file_path,status,added_lines,deleted_lines,diff_hash,created_at) VALUES (?,?,?,?,?,?,?,?)", v.ID, v.SessionID, v.FilePath, v.Status, v.AddedLines, v.DeletedLines, v.DiffHash, v.CreatedAt); e != nil {
				return e
			}
		}
		for _, v := range lines {
			if _, e := tx.ExecContext(ctx, "INSERT INTO line_provenance(id,file_path,line_identity,content_hash,source,origin_session_id,created_at) VALUES (?,?,?,?,?,?,?)", v.ID, v.FilePath, v.LineIdentity, v.ContentHash, v.Source, v.OriginSessionID, v.CreatedAt); e != nil {
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
func (s *Store) HasFinishedChange(ctx context.Context, path, except string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM ai_change_event e JOIN ai_session s ON s.id=e.session_id WHERE e.file_path=? AND e.session_id<>? AND s.state='finished'", path, except).Scan(&n)
	return n > 0, mapError(err)
}

// CurrentAIByFile returns the unremoved AI provenance rows for a file. It is
// the read side used by the verifier to map git-added lines back to sessions.
func (s *Store) CurrentAIByFile(ctx context.Context, filePath string) ([]LineProvenance, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, file_path, line_identity, content_hash, source, origin_session_id, created_at FROM line_provenance WHERE file_path=? AND source='AI' AND removed_at IS NULL", filePath)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	var out []LineProvenance
	for rows.Next() {
		var v LineProvenance
		if err = rows.Scan(&v.ID, &v.FilePath, &v.LineIdentity, &v.ContentHash, &v.Source, &v.OriginSessionID, &v.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		out = append(out, v)
	}
	return out, mapError(rows.Err())
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
