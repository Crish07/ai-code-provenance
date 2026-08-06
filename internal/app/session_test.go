package app

import (
	"ai-prov/internal/storage"
	"context"
	"os"
	"path/filepath"
	"testing"
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
	r, e := Service{root, 100, db}.Start(context.Background(), StartRequest{Task: "t", Agent: "a"})
	if e != nil || r.State != "active" || r.TrackedFiles != 1 {
		t.Fatalf("%#v %v", r, e)
	}
}
func TestServiceStart_SnapshotFailureDoesNotCreateSession(t *testing.T) {
	root := t.TempDir()
	db, e := storage.Open(filepath.Join(root, "db.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	_, e = Service{filepath.Join(root, "missing"), 100, db}.Start(context.Background(), StartRequest{Task: "t"})
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
	s := Service{root, 100, db}
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
	s := Service{root, 100, db}
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
	s := Service{root, 100, db}
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
	s := Service{root, 100, db}
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
