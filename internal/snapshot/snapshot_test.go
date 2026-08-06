package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreate(t *testing.T) {
	r := t.TempDir()
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, e := Create(r, "s1", 100)
	if e != nil || len(m.Files) != 1 {
		t.Fatalf("%#v %v", m, e)
	}
	if _, e = os.Stat(filepath.Join(r, ".ai-provenance", "snapshots", "s1", "manifest.json")); e != nil {
		t.Fatal(e)
	}
	if !Verify(r, m) {
		t.Fatal("valid snapshot rejected")
	}
	if err := os.WriteFile(filepath.Join(r, ".ai-provenance", "snapshots", "s1", "a.go"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if Verify(r, m) {
		t.Fatal("damaged snapshot accepted")
	}
}

func TestCreate_NormalizesLineEndings(t *testing.T) {
	r := t.TempDir()
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("a\r\nb\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := Create(r, "one", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Create(r, "two", 100)
	if err != nil {
		t.Fatal(err)
	}
	if first.Files[0].Hash != second.Files[0].Hash {
		t.Fatal("line endings changed hash")
	}
}
