package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpen_MigratesIdempotently(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "provenance.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='ai_session'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("ai_session table count=%d err=%v", count, err)
	}
}

func TestWithTx_RollsBackAndEnforcesConstraints(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	err = store.WithTx(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO ai_session(id,project_path,task,state,snapshot_id,started_at) VALUES ('one','p','t','active','s','now')"); err != nil {
			return err
		}
		return context.Canceled
	})
	if err == nil {
		t.Fatal("WithTx() error=nil")
	}
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM ai_session").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
	if _, err := store.db.Exec("INSERT INTO ai_session(id,project_path,task,state,snapshot_id,started_at) VALUES ('bad','p','t','other','s','now')"); err == nil {
		t.Fatal("invalid state accepted")
	}
}

func TestRepository_PersistsRelatedRecords(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	session := Session{ID: "s1", ProjectPath: "/project", Task: "task", Agent: "codex", State: "active", SnapshotID: "snap", StartedAt: Now()}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSnapshot(ctx, FileSnapshot{SnapshotID: "snap", FilePath: "a.go", ContentHash: "h", StoragePath: "snap/a"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveChangeEvent(ctx, ChangeEvent{ID: "e1", SessionID: "s1", FilePath: "a.go", Status: "modified", DiffHash: "d", CreatedAt: Now()}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLineProvenance(ctx, LineProvenance{ID: "l1", FilePath: "a.go", LineIdentity: "i", ContentHash: "h", Source: "AI", CreatedAt: Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSession(ctx, "s1")
	if err != nil || got.Task != "task" {
		t.Fatalf("GetSession() = %#v, %v", got, err)
	}
	if err := store.SaveChangeEvent(ctx, ChangeEvent{ID: "bad", SessionID: "missing", FilePath: "a", Status: "added", DiffHash: "d", CreatedAt: Now()}); err == nil {
		t.Fatal("missing foreign key accepted")
	}
}
func TestCommitFinish_RollsBackOnInvalidEvent(t *testing.T) {
	s, e := Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.CommitFinish(context.Background(), "missing", []ChangeEvent{{ID: "e", SessionID: "missing", FilePath: "a", Status: "added", DiffHash: "d", CreatedAt: Now()}}, nil); e == nil {
		t.Fatal("want error")
	}
	var n int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM ai_change_event").Scan(&n); e != nil || n != 0 {
		t.Fatalf("count=%d err=%v", n, e)
	}
}

func TestCurrentAIByFile_ReturnsUnremovedAIRows(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateSession(ctx, Session{ID: "s1", ProjectPath: "/p", Task: "t", State: "active", SnapshotID: "snap", StartedAt: Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLineProvenance(ctx, LineProvenance{ID: "l1", FilePath: "a.go", LineIdentity: "62", ContentHash: "62", Source: "AI", OriginSessionID: sql.NullString{String: "s1", Valid: true}, CreatedAt: Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLineProvenance(ctx, LineProvenance{ID: "l2", FilePath: "a.go", LineIdentity: "63", ContentHash: "63", Source: "Human", CreatedAt: Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE line_provenance SET removed_at=? WHERE id='l1'", Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveLineProvenance(ctx, LineProvenance{ID: "l3", FilePath: "a.go", LineIdentity: "64", ContentHash: "64", Source: "AI", OriginSessionID: sql.NullString{String: "s1", Valid: true}, CreatedAt: Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := s.CurrentAIByFile(ctx, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "l3" {
		t.Fatalf("got %#v want only l3", got)
	}
}
