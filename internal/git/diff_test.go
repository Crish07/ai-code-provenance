package git

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseDiff_AddedFile(t *testing.T) {
	in := []byte("diff --git a/new.go b/new.go\nnew file mode 100644\nindex 0000000..abc\n--- /dev/null\n+++ b/new.go\n@@ -0,0 +1,2 @@\n+alpha\n+beta\n")
	got, err := parseDiff(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileDiff{{Path: "new.go", AddedLines: []string{"alpha", "beta"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestParseDiff_ModifiedFileSkipsContextAndDeletes(t *testing.T) {
	in := []byte("diff --git a/f.go b/f.go\nindex abc..def 100644\n--- a/f.go\n+++ b/f.go\n@@ -1,3 +1,3 @@\n keep\n-old\n+new\n")
	got, err := parseDiff(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileDiff{{Path: "f.go", AddedLines: []string{"new"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestParseDiff_DeletedFileOmitted(t *testing.T) {
	in := []byte("diff --git a/old.go b/old.go\ndeleted file mode 100644\n--- a/old.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-gone\n")
	got, err := parseDiff(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %#v want empty", got)
	}
}

func TestParseDiff_BlankInsertsExcluded(t *testing.T) {
	in := []byte("diff --git a/f.go b/f.go\n--- a/f.go\n+++ b/f.go\n@@ -1 +1,3 @@\n code\n+\n+real\n")
	got, err := parseDiff(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileDiff{{Path: "f.go", AddedLines: []string{"real"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestParseDiff_BinaryAndRenameOmitted(t *testing.T) {
	in := []byte("diff --git a/bin b/bin\nBinary files a/bin and b/bin differ\ndiff --git a/old b/new\nsimilarity index 90%\nrename from old\nrename to new\n")
	got, err := parseDiff(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %#v want empty", got)
	}
}

func TestParseDiff_QuotedPath(t *testing.T) {
	in := []byte("diff --git \"a/p with.go\" \"b/p with.go\"\n--- \"a/p with.go\"\n+++ \"b/p with.go\"\n@@ -1 +1,2 @@\n x\n+y\n")
	got, err := parseDiff(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileDiff{{Path: "p with.go", AddedLines: []string{"y"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestParseDiff_MultipleFiles(t *testing.T) {
	in := []byte("diff --git a/a.go b/a.go\n+++ b/a.go\n@@ -1 +1,2 @@\n a\n+a1\ndiff --git a/b.go b/b.go\n+++ b/b.go\n@@ -1 +1,2 @@\n b\n+b1\n")
	got, err := parseDiff(in)
	if err != nil {
		t.Fatal(err)
	}
	want := []FileDiff{{Path: "a.go", AddedLines: []string{"a1"}}, {Path: "b.go", AddedLines: []string{"b1"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestReader_StagedScope(t *testing.T) {
	root := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")
	got, err := Reader{Root: root}.ReadDiff(context.Background(), ScopeStaged)
	if err != nil {
		t.Fatalf("ReadDiff: %v", err)
	}
	want := []FileDiff{{Path: "a.go", AddedLines: []string{"b"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestReader_WorktreeScope(t *testing.T) {
	root := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Reader{Root: root}.ReadDiff(context.Background(), ScopeWorktree)
	if err != nil {
		t.Fatalf("ReadDiff: %v", err)
	}
	want := []FileDiff{{Path: "a.go", AddedLines: []string{"b"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestReader_EmptyDiff(t *testing.T) {
	root := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "a.go")
	runGit(t, root, "commit", "-m", "init")
	got, err := Reader{Root: root}.ReadDiff(context.Background(), ScopeStaged)
	if err != nil {
		t.Fatalf("ReadDiff: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %#v want empty", got)
	}
}

func TestReader_NonGitDirIsUnavailable(t *testing.T) {
	r := Reader{Root: t.TempDir()}
	if _, err := r.ReadDiff(context.Background(), ScopeStaged); err == nil {
		t.Fatal("want ErrUnavailable")
	}
}

func TestReader_UnknownScopeIsUnavailable(t *testing.T) {
	r := Reader{Root: initGitRepo(t)}
	if _, err := r.ReadDiff(context.Background(), Scope("bogus")); err == nil {
		t.Fatal("want error for unknown scope")
	}
}

// initGitRepo creates a temporary git repository with a configured identity.
func initGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "config", "commit.gpgsign", "false")
	return root
}

// runGit executes git with -C root and fails the test on non-zero exit.
func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	var errBuf bytes.Buffer
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v in %s: %v %s", args, root, err, errBuf.String())
	}
}
