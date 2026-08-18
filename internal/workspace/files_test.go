// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScan_FiltersAndSortsFiles(t *testing.T) {
	root := t.TempDir()
	for path, data := range map[string][]byte{"b.go": []byte("b"), "a.go": []byte("a"), ".git/config": []byte("x"), "binary.dat": []byte{0, 1}, "large.txt": []byte("12345")} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("a.go", filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("b.go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, skipped, err := Scan(root, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "a.go" {
		t.Fatalf("files=%#v", files)
	}
	if len(skipped) != 4 {
		t.Fatalf("skipped=%#v", skipped)
	}
}

func TestScan_IncludesAgentMetadataDirectoriesUnlessExplicitlyIgnored(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"main.go", ".agents/docs/guide.md", ".trae/rules/ai-prov.md"} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("text"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeDefaultIgnore(t, root)
	files, _, err := Scan(root, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(files))
	for _, file := range files {
		got = append(got, file.Path)
	}
	want := []string{".agents/docs/guide.md", ".trae/rules/ai-prov.md", "main.go"}
	if !equalStrings(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

func writeDefaultIgnore(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".ai-provenance")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".ai-provenanceignore"), []byte(DefaultIgnoreRules), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScan_GitStyleIgnoreRulesAndRecursiveDirectories(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{
		"main.go":                     "main",
		"nested/main.go":              "nested",
		"root-only.txt":               "root",
		"nested/root-only.txt":        "nested root",
		"cache/drop.txt":              "drop",
		"cache/keep.txt":              "keep",
		"generated/deep/result.go":    "generated",
		".gitnexus/csv/interface.csv": "cache",
		"temporary.tmp":               "temporary",
		"nested/temporary.tmp":        "nested temporary",
	} {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("# comment\n/cache/\n!cache/keep.txt\n/root-only.txt\n*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	provenanceDir := filepath.Join(root, ".ai-provenance")
	if err := os.MkdirAll(provenanceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(provenanceDir, ".ai-provenanceignore"), []byte(DefaultIgnoreRules+".gitnexus/\ngenerated/**\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, skipped, err := Scan(root, 100)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(files))
	for _, file := range files {
		got = append(got, file.Path)
	}
	want := []string{"cache/keep.txt", "main.go", "nested/main.go", "nested/root-only.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("files=%v want %v", got, want)
	}
	for _, item := range skipped {
		if item.Path == ".gitnexus/csv/interface.csv" || item.Path == "generated/deep/result.go" {
			t.Fatalf("directory-pruned file appeared in skipped results: %#v", skipped)
		}
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
