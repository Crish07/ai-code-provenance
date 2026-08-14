// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package snapshot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateWithQuota(t *testing.T) {
	r := t.TempDir()
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, e := CreateWithQuota(r, "s1", 100, 0)
	if e != nil || len(m.Files) != 1 {
		t.Fatalf("%#v %v", m, e)
	}
	if _, e = os.Stat(filepath.Join(r, ".ai-provenance", "snapshots", "s1", "manifest.json")); e != nil {
		t.Fatal(e)
	}
	if _, err := ReadFile(r, m, m.Files[0]); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
	if m.Version != 2 {
		t.Fatalf("version=%d", m.Version)
	}
	if err := os.WriteFile(filepath.Join(r, ".ai-provenance", "objects", m.Files[0].Hash), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(r, m, m.Files[0]); err == nil {
		t.Fatal("damaged snapshot accepted")
	}
}

func TestCreateWithQuota_RejectsBeforeWritingSnapshotArtifacts(t *testing.T) {
	r := t.TempDir()
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("four"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := CreateWithQuota(r, "too-large", 100, 3)
	var quota *QuotaExceededError
	if !errors.As(err, &quota) {
		t.Fatalf("CreateWithQuota error = %v, want QuotaExceededError", err)
	}
	if quota.Existing != 0 || quota.Required != 4 || quota.Limit != 3 {
		t.Fatalf("quota=%+v", quota)
	}
	for _, path := range []string{
		filepath.Join(r, ".ai-provenance", "snapshots", "too-large"),
		filepath.Join(r, ".ai-provenance", "objects"),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("artifact %s stat error=%v, want absent", path, statErr)
		}
	}
}

func TestCreateWithQuota_AllowsExactLimitAndExistingObjectReuse(t *testing.T) {
	r := t.TempDir()
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("four"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateWithQuota(r, "one", 100, 4); err != nil {
		t.Fatalf("exact limit: %v", err)
	}
	if _, err := CreateWithQuota(r, "two", 100, 4); err != nil {
		t.Fatalf("reused object should not consume quota twice: %v", err)
	}
}

func TestCreate_DeduplicatesObjectsAndReadsLegacySnapshot(t *testing.T) {
	r := t.TempDir()
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("same\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := CreateWithQuota(r, "one", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateWithQuota(r, "two", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	objects, err := os.ReadDir(filepath.Join(r, ".ai-provenance", "objects"))
	if err != nil || len(objects) != 1 {
		t.Fatalf("objects=%#v err=%v", objects, err)
	}
	if first.Files[0].Hash != second.Files[0].Hash {
		t.Fatal("hash differs")
	}
	legacy := Manifest{ID: "legacy", Files: []File{{Path: "old.go", Hash: Hash([]byte("old\n"))}}}
	legacyDir := filepath.Join(r, ".ai-provenance", "snapshots", "legacy")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "old.go"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(r, legacy, legacy.Files[0])
	if err != nil || string(got) != "old\n" {
		t.Fatalf("legacy=%q err=%v", got, err)
	}
}

func TestGCDryRun_ProtectsSharedObjects(t *testing.T) {
	r := t.TempDir()
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := CreateWithQuota(r, "one", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateWithQuota(r, "two", 100, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("unique\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := CreateWithQuota(r, "three", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	report, err := GCDryRun(r, []string{"one", "three"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.SessionIDs) != 2 || report.SessionIDs[0] != "one" || report.SessionIDs[1] != "three" {
		t.Fatalf("sessions=%#v", report)
	}
	if len(report.ObjectHashes) != 1 || report.ObjectHashes[0] != third.Files[0].Hash {
		t.Fatalf("objects=%#v shared=%s", report, first.Files[0].Hash)
	}
}

func TestGCApply_RemovesOnlyCandidateSnapshotAndObject(t *testing.T) {
	r := t.TempDir()
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := CreateWithQuota(r, "old", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	keep, err := CreateWithQuota(r, "keep", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GCApply(r, []string{"old"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r, ".ai-provenance", "snapshots", "old")); !os.IsNotExist(err) {
		t.Fatalf("old snapshot=%v", err)
	}
	if _, err := os.Stat(filepath.Join(r, ".ai-provenance", "objects", old.Files[0].Hash)); !os.IsNotExist(err) {
		t.Fatalf("old object=%v", err)
	}
	if _, err := ReadFile(r, keep, keep.Files[0]); err != nil {
		t.Fatal(err)
	}
}

func TestCreate_NormalizesLineEndings(t *testing.T) {
	r := t.TempDir()
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("a\r\nb\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := CreateWithQuota(r, "one", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r, "a.go"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := CreateWithQuota(r, "two", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Files[0].Hash != second.Files[0].Hash {
		t.Fatal("line endings changed hash")
	}
}

func TestMatches_UsesCanonicalContentHash(t *testing.T) {
	baseline := Normalize([]byte("a\r\nb\r\n"))
	if !Matches(Normalize([]byte("a\nb\n")), Hash(baseline)) {
		t.Fatal("equivalent line endings should match")
	}
	if Matches(Normalize([]byte("a\nc\n")), Hash(baseline)) {
		t.Fatal("changed content should not match")
	}
}

func TestNormalize_LFReturnsOriginalBuffer(t *testing.T) {
	input := []byte("one\ntwo\n")
	got := Normalize(input)
	if string(got) != string(input) {
		t.Fatalf("Normalize() = %q, want %q", got, input)
	}
	if &got[0] != &input[0] {
		t.Fatal("Normalize copied LF-only content")
	}
}
