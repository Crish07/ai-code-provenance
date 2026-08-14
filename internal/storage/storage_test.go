// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

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

func TestOpen_UpgradesV1AndExcludesLegacyProvenance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provenance.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"CREATE TABLE schema_migration (version INTEGER PRIMARY KEY)",
		"INSERT INTO schema_migration(version) VALUES (1)",
		"CREATE TABLE ai_session (id TEXT PRIMARY KEY, project_path TEXT NOT NULL, task TEXT NOT NULL, agent TEXT NOT NULL DEFAULT 'unknown', model TEXT, state TEXT NOT NULL CHECK (state IN ('active','finished','failed')), snapshot_id TEXT NOT NULL, started_at TEXT NOT NULL, finished_at TEXT, failure_code TEXT, failure_message TEXT)",
		"CREATE TABLE line_provenance (id TEXT PRIMARY KEY, file_path TEXT NOT NULL, line_identity TEXT NOT NULL, content_hash TEXT NOT NULL, source TEXT NOT NULL CHECK (source IN ('AI','Human','Unknown')), origin_session_id TEXT REFERENCES ai_session(id), created_at TEXT NOT NULL, removed_at TEXT)",
		"INSERT INTO line_provenance(id,file_path,line_identity,content_hash,source,created_at) VALUES ('legacy','a.go','old-hex','old-hex','AI','now')",
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version, identityVersion int
	if err := store.db.QueryRow("SELECT MAX(version) FROM schema_migration").Scan(&version); err != nil || version != schemaVersion {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	if err := store.db.QueryRow("SELECT identity_version FROM line_provenance WHERE id='legacy'").Scan(&identityVersion); err != nil || identityVersion != 1 {
		t.Fatalf("legacy identity version=%d err=%v", identityVersion, err)
	}
	got, err := store.CurrentAIByFile(context.Background(), "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("legacy rows must not provide v2 coverage: %#v", got)
	}
	var instance, heartbeat string
	if err := store.db.QueryRow("SELECT agent_instance_id,last_heartbeat_at FROM ai_session LIMIT 1").Scan(&instance, &heartbeat); err != sql.ErrNoRows {
		t.Fatalf("legacy session migration query err=%v, want no rows", err)
	}
}

func TestOpen_UpgradesV2SessionInstanceFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provenance.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"CREATE TABLE schema_migration (version INTEGER PRIMARY KEY)",
		"INSERT INTO schema_migration(version) VALUES (2)",
		"CREATE TABLE ai_session (id TEXT PRIMARY KEY, project_path TEXT NOT NULL, task TEXT NOT NULL, agent TEXT NOT NULL DEFAULT 'unknown', model TEXT, state TEXT NOT NULL CHECK (state IN ('active','finished','failed')), snapshot_id TEXT NOT NULL, started_at TEXT NOT NULL, finished_at TEXT, failure_code TEXT, failure_message TEXT)",
		"INSERT INTO ai_session(id,project_path,task,agent,state,snapshot_id,started_at) VALUES ('legacy','/p','t','codex','active','s','2026-01-01T00:00:00Z')",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.GetSession(context.Background(), "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentInstanceID != "legacy" || !got.LastHeartbeatAt.Valid || got.LastHeartbeatAt.String != got.StartedAt {
		t.Fatalf("upgraded session=%#v", got)
	}
}

func TestStore_HeartbeatAndExpireLeasesRespectInstanceAndCutoff(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	old := "2026-01-01T00:00:00Z"
	for _, session := range []Session{
		{ID: "old", ProjectPath: "/p", Task: "t", Agent: "a", AgentInstanceID: "one", State: "active", SnapshotID: "s1", StartedAt: old, LastHeartbeatAt: sql.NullString{String: old, Valid: true}},
		{ID: "fresh", ProjectPath: "/p", Task: "t", Agent: "a", AgentInstanceID: "two", State: "active", SnapshotID: "s2", StartedAt: old, LastHeartbeatAt: sql.NullString{String: "2027-01-01T00:00:00Z", Valid: true}},
	} {
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := store.Heartbeat(ctx, "old", "wrong", "2027-01-01T00:00:00Z"); err != nil || ok {
		t.Fatalf("wrong heartbeat ok=%v err=%v", ok, err)
	}
	n, err := store.ExpireLeases(ctx, "/p", "2026-06-01T00:00:00Z")
	if err != nil || n != 1 {
		t.Fatalf("expired=%d err=%v", n, err)
	}
	got, err := store.GetSession(ctx, "old")
	if err != nil || got.State != "failed" || got.FailureCode.String != "SESSION_LEASE_EXPIRED" {
		t.Fatalf("old=%#v err=%v", got, err)
	}
	fresh, _ := store.GetSession(ctx, "fresh")
	if fresh.State != "active" {
		t.Fatalf("fresh=%#v", fresh)
	}
}

func TestStore_MaintenanceLeaseThrottlesAndRecoversExpiredOwner(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	acquired, err := store.TryAcquireMaintenance(ctx, SnapshotGCMaintenance, "one", "2026-08-14T00:00:00Z", "2026-08-14T00:10:00Z", "2026-08-13T00:00:00Z")
	if err != nil || !acquired {
		t.Fatalf("first acquire = %v, %v", acquired, err)
	}
	acquired, err = store.TryAcquireMaintenance(ctx, SnapshotGCMaintenance, "two", "2026-08-14T00:01:00Z", "2026-08-14T00:11:00Z", "2026-08-13T00:01:00Z")
	if err != nil || acquired {
		t.Fatalf("concurrent acquire = %v, %v, want false,nil", acquired, err)
	}
	acquired, err = store.TryAcquireMaintenance(ctx, SnapshotGCMaintenance, "two", "2026-08-14T00:11:00Z", "2026-08-14T00:21:00Z", "2026-08-13T00:11:00Z")
	if err != nil || !acquired {
		t.Fatalf("expired lease takeover = %v, %v", acquired, err)
	}
	if err := store.CompleteMaintenance(ctx, SnapshotGCMaintenance, "two", "2026-08-14T00:12:00Z"); err != nil {
		t.Fatal(err)
	}
	acquired, err = store.TryAcquireMaintenance(ctx, SnapshotGCMaintenance, "three", "2026-08-14T12:00:00Z", "2026-08-14T12:10:00Z", "2026-08-13T12:00:00Z")
	if err != nil || acquired {
		t.Fatalf("interval throttle acquire = %v, %v, want false,nil", acquired, err)
	}
	acquired, err = store.TryAcquireMaintenance(ctx, SnapshotGCMaintenance, "three", "2026-08-15T00:13:00Z", "2026-08-15T00:23:00Z", "2026-08-14T00:13:00Z")
	if err != nil || !acquired {
		t.Fatalf("next interval acquire = %v, %v", acquired, err)
	}
}

func TestStore_MaintenanceFailureKeepsIntervalThrottle(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if acquired, err := store.TryAcquireMaintenance(ctx, SnapshotGCMaintenance, "one", "2026-08-14T00:00:00Z", "2026-08-14T00:10:00Z", "2026-08-13T00:00:00Z"); err != nil || !acquired {
		t.Fatalf("first acquire = %v, %v", acquired, err)
	}
	if err := store.FailMaintenance(ctx, SnapshotGCMaintenance, "one", "snapshot gc failed"); err != nil {
		t.Fatal(err)
	}
	if acquired, err := store.TryAcquireMaintenance(ctx, SnapshotGCMaintenance, "two", "2026-08-14T01:00:00Z", "2026-08-14T01:10:00Z", "2026-08-13T01:00:00Z"); err != nil || acquired {
		t.Fatalf("retry after failure = %v, %v, want false,nil", acquired, err)
	}
	var message string
	if err := store.db.QueryRow("SELECT last_error FROM maintenance_state WHERE name=?", SnapshotGCMaintenance).Scan(&message); err != nil || message != "snapshot gc failed" {
		t.Fatalf("last_error = %q, %v", message, err)
	}
}

func TestLineProvenanceSchema_EnforcesOneCurrentV2Identity(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	first := LineProvenance{ID: "first", FilePath: "a.go", LineIdentity: "identity", ContentHash: "hash", Source: "AI", CreatedAt: Now()}
	if err := insertLine(ctx, store, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = "second"
	if err := insertLine(ctx, store, second); err == nil {
		t.Fatal("duplicate current v2 identity accepted")
	}
	if _, err := store.db.Exec("UPDATE line_provenance SET removed_at=? WHERE id='first'", Now()); err != nil {
		t.Fatal(err)
	}
	if err := insertLine(ctx, store, second); err != nil {
		t.Fatalf("removed identity should allow replacement: %v", err)
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
	session := Session{ID: "s1", ProjectPath: "/project", Task: "task", Agent: "codex", AgentInstanceID: "instance", State: "active", SnapshotID: "snap", StartedAt: Now(), LastHeartbeatAt: sql.NullString{String: Now(), Valid: true}}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "INSERT INTO ai_change_event(id,session_id,file_path,status,added_lines,deleted_lines,diff_hash,created_at) VALUES (?,?,?,?,?,?,?,?)", "e1", "s1", "a.go", "modified", 0, 0, "d", Now()); err != nil {
		t.Fatal(err)
	}
	if err := insertLine(ctx, store, LineProvenance{ID: "l1", FilePath: "a.go", LineIdentity: "i", ContentHash: "h", Source: "AI", CreatedAt: Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSession(ctx, "s1")
	if err != nil || got.Task != "task" || got.AgentInstanceID != "instance" {
		t.Fatalf("GetSession() = %#v, %v", got, err)
	}
	if _, err := store.db.ExecContext(ctx, "INSERT INTO ai_change_event(id,session_id,file_path,status,added_lines,deleted_lines,diff_hash,created_at) VALUES (?,?,?,?,?,?,?,?)", "bad", "missing", "a", "added", 0, 0, "d", Now()); err == nil {
		t.Fatal("missing foreign key accepted")
	}
}

func TestOpen_UpgradesV3AndDropsLegacyFileSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provenance.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"CREATE TABLE schema_migration (version INTEGER PRIMARY KEY)",
		"INSERT INTO schema_migration(version) VALUES (3)",
		"CREATE TABLE file_snapshot (snapshot_id TEXT NOT NULL, file_path TEXT NOT NULL, content_hash TEXT NOT NULL, storage_path TEXT NOT NULL, PRIMARY KEY (snapshot_id, file_path))",
		"INSERT INTO file_snapshot(snapshot_id,file_path,content_hash,storage_path) VALUES ('legacy','a.go','hash','path')",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='file_snapshot'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("legacy file_snapshot count=%d err=%v", count, err)
	}
}

func TestCommitFinish_RollsBackOnInvalidEvent(t *testing.T) {
	s, e := Open(":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.CommitFinish(context.Background(), "missing", []ChangeEvent{{ID: "e", SessionID: "missing", FilePath: "a", Status: "added", DiffHash: "d", CreatedAt: Now()}}, nil, nil); e == nil {
		t.Fatal("want error")
	}
	var n int
	if e = s.db.QueryRow("SELECT COUNT(*) FROM ai_change_event").Scan(&n); e != nil || n != 0 {
		t.Fatalf("count=%d err=%v", n, e)
	}
}

func TestCommitFinish_RollsBackProvenanceRemoval(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := insertLine(ctx, s, LineProvenance{ID: "old", FilePath: "a.go", LineIdentity: "old", ContentHash: "hash", Source: "AI", CreatedAt: Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitFinish(ctx, "missing", []ChangeEvent{{ID: "event", SessionID: "missing", FilePath: "a.go", Status: "modified", DiffHash: "hash", CreatedAt: Now()}}, nil, []string{"old"}); err == nil {
		t.Fatal("want error")
	}
	var removed sql.NullString
	if err := s.db.QueryRow("SELECT removed_at FROM line_provenance WHERE id='old'").Scan(&removed); err != nil {
		t.Fatal(err)
	}
	if removed.Valid {
		t.Fatal("failed finish left removed_at committed")
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
	if err := insertLine(ctx, s, LineProvenance{ID: "l1", FilePath: "a.go", LineIdentity: "62", ContentHash: "62", Source: "AI", OriginSessionID: sql.NullString{String: "s1", Valid: true}, CreatedAt: Now()}); err != nil {
		t.Fatal(err)
	}
	if err := insertLine(ctx, s, LineProvenance{ID: "l2", FilePath: "a.go", LineIdentity: "63", ContentHash: "63", Source: "Human", CreatedAt: Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("UPDATE line_provenance SET removed_at=? WHERE id='l1'", Now()); err != nil {
		t.Fatal(err)
	}
	if err := insertLine(ctx, s, LineProvenance{ID: "l3", FilePath: "a.go", LineIdentity: "64", ContentHash: "64", Source: "AI", OriginSessionID: sql.NullString{String: "s1", Valid: true}, CreatedAt: Now()}); err != nil {
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

func TestActiveSessions_FiltersProjectAndSorts(t *testing.T) {
	store, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, session := range []Session{
		{ID: "later", ProjectPath: "/one", Task: "later", State: "active", SnapshotID: "s2", StartedAt: "2026-08-11T02:00:00Z"},
		{ID: "other", ProjectPath: "/two", Task: "other", State: "active", SnapshotID: "s3", StartedAt: "2026-08-11T00:00:00Z"},
		{ID: "first", ProjectPath: "/one", Task: "first", State: "active", SnapshotID: "s1", StartedAt: "2026-08-11T01:00:00Z"},
	} {
		if err := store.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.ActiveSessions(ctx, "/one")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "first" || got[1].ID != "later" {
		t.Fatalf("sessions=%#v", got)
	}
}

func insertLine(ctx context.Context, store *Store, v LineProvenance) error {
	if v.IdentityVersion == 0 {
		v.IdentityVersion = IdentityVersionV2
	}
	_, err := store.db.ExecContext(ctx, "INSERT INTO line_provenance(id,file_path,line_identity,content_hash,source,origin_session_id,created_at,identity_version) VALUES (?,?,?,?,?,?,?,?)", v.ID, v.FilePath, v.LineIdentity, v.ContentHash, v.Source, v.OriginSessionID, v.CreatedAt, v.IdentityVersion)
	return err
}
