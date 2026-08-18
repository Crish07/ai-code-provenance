// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package app

import (
	"ai-prov/internal/snapshot"
	"ai-prov/internal/storage"
	"ai-prov/internal/workspace"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServiceStart(t *testing.T) {
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "a.go"), []byte("x"), 0o644); e != nil {
		t.Fatal(e)
	}
	db, e := storage.Open(filepath.Join(root, "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	r, e := Service{Root: root, MaxFileBytes: 100, Store: db}.Start(context.Background(), StartRequest{Task: "t", Agent: "a"})
	if e != nil || r.State != "active" || r.TrackedFiles != 1 {
		t.Fatalf("%#v %v", r, e)
	}
	if r.AgentInstanceID == "" {
		t.Fatal("Start did not generate agent instance ID")
	}
}

func TestServiceStart_PersistsProvidedAgentInstanceAndTaskKey(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	instance := "a1234567-1234-4234-8234-123456789abc"
	start, err := (Service{Root: root, MaxFileBytes: 100, Store: db}).Start(context.Background(), StartRequest{Task: "t", AgentInstanceID: instance, TaskKey: "task-1"})
	if err != nil {
		t.Fatal(err)
	}
	if start.AgentInstanceID != instance {
		t.Fatalf("start=%#v", start)
	}
	stored, err := db.GetSession(context.Background(), start.SessionID)
	if err != nil || stored.AgentInstanceID != instance || !stored.TaskKey.Valid || stored.TaskKey.String != "task-1" || !stored.LastHeartbeatAt.Valid {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
}

func TestServiceStart_ReclaimsExpiredTerminalSnapshotsByRetention(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	initial := Service{Root: root, MaxFileBytes: 100, Store: store}
	old, err := initial.Start(context.Background(), StartRequest{Task: "old"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := initial.Abandon(context.Background(), old.SessionID, "test terminal retention"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	reclaimer := Service{Root: root, MaxFileBytes: 100, SnapshotRetention: time.Nanosecond, SnapshotAutoGCInterval: time.Nanosecond, Store: store}
	current, err := reclaimer.Start(context.Background(), StartRequest{Task: "new"})
	if err != nil {
		t.Fatal(err)
	}
	waitForSnapshotGone(t, filepath.Join(root, ".ai-provenance", "snapshots", old.SnapshotID))
	if _, err := os.Stat(filepath.Join(root, ".ai-provenance", "snapshots", current.SnapshotID)); err != nil {
		t.Fatalf("new active snapshot missing: %v", err)
	}
}

func TestService_EnforcesInstanceLimitAndOwner(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := Service{Root: root, MaxFileBytes: 100, Store: db, MaxActivePerAgentInstance: 1}
	instance := "a1234567-1234-4234-8234-123456789abc"
	first, err := svc.Start(context.Background(), StartRequest{Task: "one", AgentInstanceID: instance})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), StartRequest{Task: "two", AgentInstanceID: instance}); !errors.Is(err, ErrActiveSessionLimit) {
		t.Fatalf("limit error=%v", err)
	}
	other := "b1234567-1234-4234-8234-123456789abc"
	if _, err := svc.Heartbeat(context.Background(), first.SessionID, other); !errors.Is(err, ErrSessionOwnerMismatch) {
		t.Fatalf("heartbeat owner error=%v", err)
	}
	if _, err := svc.AbandonOwned(context.Background(), first.SessionID, other, "no"); !errors.Is(err, ErrSessionOwnerMismatch) {
		t.Fatalf("abandon owner error=%v", err)
	}
	if _, err := svc.AbandonOwned(context.Background(), first.SessionID, instance, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), StartRequest{Task: "three", AgentInstanceID: instance}); err != nil {
		t.Fatal(err)
	}
}

func TestService_MaintenanceReclaimsOnlyExpiredLeaseSnapshotAfterGrace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	base := Service{Root: root, MaxFileBytes: 100, Store: db}
	expired, err := base.Start(context.Background(), StartRequest{Task: "expired", AgentInstanceID: "a1234567-1234-4234-8234-123456789abc"})
	if err != nil {
		t.Fatal(err)
	}
	active, err := base.Start(context.Background(), StartRequest{Task: "active", AgentInstanceID: "b1234567-1234-4234-8234-123456789abc"})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := db.Heartbeat(context.Background(), active.SessionID, active.AgentInstanceID, "9999-01-01T00:00:00Z"); err != nil || !ok {
		t.Fatalf("refresh active lease ok=%v err=%v", ok, err)
	}
	if _, err := db.ExpireLeases(context.Background(), root, "9998-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	maintained := Service{Root: root, MaxFileBytes: 100, Store: db, ExpiredSessionGrace: time.Nanosecond, AutoReclaimExpiredSessions: true, SnapshotAutoGCInterval: time.Nanosecond}
	if _, err := maintained.Start(context.Background(), StartRequest{Task: "trigger", AgentInstanceID: "c1234567-1234-4234-8234-123456789abc"}); err != nil {
		t.Fatal(err)
	}
	waitForSnapshotGone(t, filepath.Join(root, ".ai-provenance", "snapshots", expired.SnapshotID))
	if _, err := os.Stat(filepath.Join(root, ".ai-provenance", "snapshots", active.SnapshotID)); err != nil {
		t.Fatalf("active snapshot removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".ai-provenance", "objects")); err != nil {
		t.Fatalf("shared objects removed: %v", err)
	}
}

func waitForSnapshotGone(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("snapshot still exists: %s", path)
}

func TestServiceStart_QuotaFailureDoesNotCreateSession(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("four"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = (Service{Root: root, MaxFileBytes: 100, MaxSnapshotBytes: 3, Store: db}).Start(context.Background(), StartRequest{Task: "quota"})
	var quota *snapshot.QuotaExceededError
	if !errors.As(err, &quota) {
		t.Fatalf("Start error = %v, want QuotaExceededError", err)
	}
	active, finished, failed, err := db.SessionCounts(context.Background())
	if err != nil || active != 0 || finished != 0 || failed != 0 {
		t.Fatalf("session counts = %d/%d/%d, err=%v", active, finished, failed, err)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".ai-provenance", "snapshots")); !os.IsNotExist(statErr) {
		t.Fatalf("snapshot artifact stat error=%v, want absent", statErr)
	}
}
func TestServiceStart_SnapshotFailureDoesNotCreateSession(t *testing.T) {
	root := t.TempDir()
	db, e := storage.Open(filepath.Join(root, "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	_, e = Service{Root: filepath.Join(root, "missing"), MaxFileBytes: 100, Store: db}.Start(context.Background(), StartRequest{Task: "t"})
	if e == nil {
		t.Fatal("want error")
	}
	a, _, _, e := db.SessionCounts(context.Background())
	if e != nil || a != 0 {
		t.Fatalf("sessions=%d err=%v", a, e)
	}
}
func TestServiceFinish(t *testing.T) {
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "a.go"), []byte("x"), 0o644); e != nil {
		t.Fatal(e)
	}
	db, e := storage.Open(filepath.Join(root, "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := Service{Root: root, MaxFileBytes: 100, Store: db}
	start, e := s.Start(context.Background(), StartRequest{Task: "t"})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Finish(context.Background(), start.SessionID)
	if e != nil || r.State != "finished" {
		t.Fatalf("%#v %v", r, e)
	}
	if _, e = s.Finish(context.Background(), start.SessionID); e == nil {
		t.Fatal("repeat finish")
	}
}

func TestServiceFinish_IgnoresGitNexusCacheBeyondDiffLimit(t *testing.T) {
	root := t.TempDir()
	codePath := filepath.Join(root, "main.go")
	if err := os.WriteFile(codePath, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := Service{Root: root, MaxFileBytes: 2 * 1024 * 1024, Store: db}
	cachePath := filepath.Join(root, ".gitnexus", "csv", "interface.csv")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(strings.Repeat("generated,row\n", 5_000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".ai-provenance"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ai-provenance", ".ai-provenanceignore"), []byte(workspace.DefaultIgnoreRules), 0o644); err != nil {
		t.Fatal(err)
	}
	start, err := svc.Start(context.Background(), StartRequest{Task: "edit code"})
	if err != nil {
		t.Fatal(err)
	}
	if start.TrackedFiles != 1 {
		t.Fatalf("start tracked files = %d, want 1", start.TrackedFiles)
	}
	if err := os.WriteFile(codePath, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(strings.Repeat("updated,row\n", 5_000)), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Finish(context.Background(), start.SessionID)
	if err != nil {
		t.Fatalf("Finish error = %v", err)
	}
	if result.State != "finished" || result.ChangedFiles != 1 || len(result.Changes) != 1 || result.Changes[0].Path != "main.go" {
		t.Fatalf("Finish result = %#v", result)
	}
}
func TestServiceFinish_RecordsEdit(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.go")
	if e := os.WriteFile(p, []byte("a\n"), 0o644); e != nil {
		t.Fatal(e)
	}
	db, e := storage.Open(filepath.Join(root, "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := Service{Root: root, MaxFileBytes: 100, Store: db}
	start, e := s.Start(context.Background(), StartRequest{Task: "t"})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, []byte("a\nb\n"), 0o644); e != nil {
		t.Fatal(e)
	}
	r, e := s.Finish(context.Background(), start.SessionID)
	if e != nil || r.ChangedFiles != 1 || r.AddedLines != 1 {
		t.Fatalf("%#v %v", r, e)
	}
}

func TestService_AbandonMarksOnlySelectedActiveSessionFailed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := Service{Root: root, MaxFileBytes: 100, Store: db}
	first, err := svc.Start(context.Background(), StartRequest{Task: "one"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Start(context.Background(), StartRequest{Task: "two"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Abandon(context.Background(), first.SessionID, "lost after compaction")
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "failed" || result.FailureCode != "SESSION_ABANDONED" {
		t.Fatalf("result=%#v", result)
	}
	remaining, err := svc.ActiveSessions(context.Background())
	if err != nil || len(remaining) != 1 || remaining[0].SessionID != second.SessionID {
		t.Fatalf("remaining=%#v err=%v", remaining, err)
	}
}

func TestServiceFinish_ClassifiesAddedAndDeletedFiles(t *testing.T) {
	root := t.TempDir()
	deleted := filepath.Join(root, "deleted.go")
	if err := os.WriteFile(deleted, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := Service{Root: root, MaxFileBytes: 100, Store: db}
	start, err := svc.Start(context.Background(), StartRequest{Task: "file states"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(deleted); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "added.go"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Finish(context.Background(), start.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedFiles != 2 || result.AddedLines != 1 || result.DeletedLines != 1 {
		t.Fatalf("result=%#v", result)
	}
	if result.Changes[0].Path != "added.go" || result.Changes[0].Status != "added" || result.Changes[1].Path != "deleted.go" || result.Changes[1].Status != "deleted" {
		t.Fatalf("changes=%#v", result.Changes)
	}
}

func TestServiceFinish_DeletedFileInvalidatesCurrentProvenance(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := Service{Root: root, MaxFileBytes: 100, Store: db}
	first, err := svc.Start(context.Background(), StartRequest{Task: "add"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("base\nai\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finish(context.Background(), first.SessionID); err != nil {
		t.Fatal(err)
	}
	if rows, err := db.CurrentAIByFile(context.Background(), "a.go"); err != nil || len(rows) != 1 {
		t.Fatalf("initial rows=%#v err=%v", rows, err)
	}
	second, err := svc.Start(context.Background(), StartRequest{Task: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Finish(context.Background(), second.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 1 || result.Changes[0].Status != "deleted" {
		t.Fatalf("result=%#v", result)
	}
	if rows, err := db.CurrentAIByFile(context.Background(), "a.go"); err != nil || len(rows) != 0 {
		t.Fatalf("current rows=%#v err=%v", rows, err)
	}
}

func TestServiceFinish_RenameCandidateIsAddedAndDeleted(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "old.go"), []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := Service{Root: root, MaxFileBytes: 100, Store: db}
	start, err := svc.Start(context.Background(), StartRequest{Task: "rename"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "old.go"), filepath.Join(root, "new.go")); err != nil {
		t.Fatal(err)
	}
	result, err := svc.Finish(context.Background(), start.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 2 || result.Changes[0].Path != "new.go" || result.Changes[0].Status != "added" || result.Changes[1].Path != "old.go" || result.Changes[1].Status != "deleted" {
		t.Fatalf("changes=%#v", result.Changes)
	}
}

func TestServiceFinish_MigratesEqualProvenanceAndInvalidatesDeletion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := Service{Root: root, MaxFileBytes: 100, Store: db}

	first, err := svc.Start(context.Background(), StartRequest{Task: "add AI line"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("one\ntwo\nai\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finish(context.Background(), first.SessionID); err != nil {
		t.Fatal(err)
	}
	beforeMove, err := db.CurrentAIByFile(context.Background(), "a.go")
	if err != nil || len(beforeMove) != 1 {
		t.Fatalf("initial current provenance = %#v, %v", beforeMove, err)
	}

	second, err := svc.Start(context.Background(), StartRequest{Task: "insert head"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("head\none\ntwo\nai\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finish(context.Background(), second.SessionID); err != nil {
		t.Fatal(err)
	}
	afterMove, err := db.CurrentAIByFile(context.Background(), "a.go")
	if err != nil || len(afterMove) != 2 {
		t.Fatalf("migrated current provenance = %#v, %v", afterMove, err)
	}
	var migrated storage.LineProvenance
	for _, row := range afterMove {
		if row.OriginSessionID.Valid && row.OriginSessionID.String == first.SessionID {
			migrated = row
		}
	}
	if migrated.ID == "" || migrated.ID == beforeMove[0].ID {
		t.Fatalf("equal AI line was not migrated as a new current record: before=%#v after=%#v", beforeMove, afterMove)
	}
	third, err := svc.Start(context.Background(), StartRequest{Task: "delete AI line"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("head\none\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finish(context.Background(), third.SessionID); err != nil {
		t.Fatal(err)
	}
	current, err := db.CurrentAIByFile(context.Background(), "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 1 || !current[0].OriginSessionID.Valid || current[0].OriginSessionID.String != second.SessionID {
		t.Fatalf("current provenance after delete = %#v", current)
	}
}

func TestServiceFinish_CancelledMarksSessionFailedWithoutProvenance(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := Service{Root: root, MaxFileBytes: 100, Store: db}
	start, err := svc.Start(context.Background(), StartRequest{Task: "cancel"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("before\nafter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.Finish(ctx, start.SessionID)
	if !errors.Is(err, ErrFinishCancelled) {
		t.Fatalf("Finish error = %v, want ErrFinishCancelled", err)
	}
	session, err := db.GetSession(context.Background(), start.SessionID)
	if err != nil || session.State != "failed" || session.FailureCode.String != "FINISH_CANCELLED" {
		t.Fatalf("session = %#v, err = %v", session, err)
	}
	rows, err := db.CurrentAIByFile(context.Background(), "a.go")
	if err != nil || len(rows) != 0 {
		t.Fatalf("partial current provenance: rows=%#v err=%v", rows, err)
	}
}

func TestServiceFinish_HashSkipsUnchangedLargeFile(t *testing.T) {
	root := t.TempDir()
	large := filepath.Join(root, "unchanged.txt")
	var content strings.Builder
	for i := 0; i < 2_000; i++ {
		content.WriteString("unchanged line\n")
	}
	if e := os.WriteFile(large, []byte(content.String()), 0o644); e != nil {
		t.Fatal(e)
	}
	changedPath := filepath.Join(root, "changed.go")
	if e := os.WriteFile(changedPath, []byte("package changed\n"), 0o644); e != nil {
		t.Fatal(e)
	}
	db, e := storage.Open(filepath.Join(root, "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := Service{Root: root, MaxFileBytes: 1 << 20, Store: db}
	start, e := s.Start(context.Background(), StartRequest{Task: "small edit"})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(changedPath, []byte("package changed\n// edited\n"), 0o644); e != nil {
		t.Fatal(e)
	}
	result, e := s.Finish(context.Background(), start.SessionID)
	if e != nil || result.ChangedFiles != 1 || result.Changes[0].Path != "changed.go" {
		t.Fatalf("finish = %#v, err = %v", result, e)
	}
}

func TestServiceFinish_ConcurrentScanPreservesManifestOrder(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"z.go", "a.go", "m.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("before\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := Service{Root: root, MaxFileBytes: 100, Store: db}
	start, err := svc.Start(context.Background(), StartRequest{Task: "ordered changes"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"z.go", "a.go", "m.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("before\nafter\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := svc.Finish(context.Background(), start.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 3 {
		t.Fatalf("changes = %#v", result.Changes)
	}
	for i, want := range []string{"a.go", "m.go", "z.go"} {
		if result.Changes[i].Path != want {
			t.Fatalf("change %d path = %q, want %q", i, result.Changes[i].Path, want)
		}
	}
}

func TestServiceFinish_DiffResourceLimitDoesNotCommit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.go")
	var before, after strings.Builder
	for i := 0; i < 2_100; i++ {
		before.WriteString("before\n")
		after.WriteString("after\n")
	}
	if err := os.WriteFile(path, []byte(before.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(filepath.Join(root, "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := Service{Root: root, MaxFileBytes: 1 << 20, Store: db}
	start, err := svc.Start(context.Background(), StartRequest{Task: "limit"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(after.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Finish(context.Background(), start.SessionID); !errors.Is(err, ErrDiffResourceLimit) {
		t.Fatalf("Finish error = %v", err)
	}
	rows, err := db.CurrentAIByFile(context.Background(), "large.go")
	if err != nil || len(rows) != 0 {
		t.Fatalf("partial current provenance: rows=%#v err=%v", rows, err)
	}
	session, err := db.GetSession(context.Background(), start.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if session.State != "failed" || !session.FailureCode.Valid || session.FailureCode.String != "DIFF_RESOURCE_LIMIT" {
		t.Fatalf("session = %#v, want failed with DIFF_RESOURCE_LIMIT", session)
	}
}

func TestServiceFinish_ChinesePathWithCRLFAndNoEdit(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs")
	if e := os.MkdirAll(dir, 0o755); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "瑞达AI协作执行手册.md")
	if e := os.WriteFile(path, []byte("# 手册\r\n\r\n内容\r\n"), 0o644); e != nil {
		t.Fatal(e)
	}
	db, e := storage.Open(filepath.Join(root, "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := Service{Root: root, MaxFileBytes: 100, Store: db}
	start, e := s.Start(context.Background(), StartRequest{Task: "no edit"})
	if e != nil {
		t.Fatal(e)
	}
	if start.TrackedFiles != 1 {
		t.Fatalf("tracked files = %d, want Chinese path tracked", start.TrackedFiles)
	}
	result, e := s.Finish(context.Background(), start.SessionID)
	if e != nil || result.State != "finished" || result.ChangedFiles != 0 {
		t.Fatalf("finish = %#v, err = %v", result, e)
	}
}
func TestServiceFinish_FailsForMissingSnapshot(t *testing.T) {
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "a.go"), []byte("x"), 0o644); e != nil {
		t.Fatal(e)
	}
	db, e := storage.Open(filepath.Join(root, "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := Service{Root: root, MaxFileBytes: 100, Store: db}
	start, e := s.Start(context.Background(), StartRequest{Task: "t"})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(filepath.Join(root, ".ai-provenance", "snapshots", start.SnapshotID, "manifest.json")); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Finish(context.Background(), start.SessionID); e == nil {
		t.Fatal("want error")
	}
	v, e := db.GetSession(context.Background(), start.SessionID)
	if e != nil || v.State != "failed" {
		t.Fatalf("%#v %v", v, e)
	}
}
func TestServiceFinish_RejectsConflictingSession(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a.go")
	if e := os.WriteFile(p, []byte("a\n"), 0o644); e != nil {
		t.Fatal(e)
	}
	db, e := storage.Open(filepath.Join(root, "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := Service{Root: root, MaxFileBytes: 100, Store: db}
	one, e := s.Start(context.Background(), StartRequest{Task: "one"})
	if e != nil {
		t.Fatal(e)
	}
	two, e := s.Start(context.Background(), StartRequest{Task: "two"})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, []byte("a\nb\n"), 0o644); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Finish(context.Background(), one.SessionID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Finish(context.Background(), two.SessionID); e == nil {
		t.Fatal("want conflict")
	}
}
