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

func TestScan_SkipsAgentMetadataDirectories(t *testing.T) {
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
	files, _, err := Scan(root, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "main.go" {
		t.Fatalf("files = %#v, want only main.go", files)
	}
}
