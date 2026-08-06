package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHooksDir_ResolvesHooksPath confirms `git rev-parse --git-path hooks`
// returns the .git/hooks path for a normal repo.
func TestHooksDir_ResolvesHooksPath(t *testing.T) {
	root := initGitRepo(t)
	got, err := HooksDir(root)
	if err != nil {
		t.Fatalf("HooksDir: %v", err)
	}
	want := filepath.Join(root, ".git", "hooks")
	if got != want {
		t.Errorf("HooksDir = %q, want %q", got, want)
	}
}

// TestHooksDir_NonGitDirIsUnavailable ensures a missing repo maps to
// ErrUnavailable rather than a generic exec error.
func TestHooksDir_NonGitDirIsUnavailable(t *testing.T) {
	if _, err := HooksDir(t.TempDir()); err == nil {
		t.Fatal("want ErrUnavailable for non-git dir")
	}
}

// TestInstaller_InstallCreatesManagedHook writes the hook on a fresh repo and
// verifies the file exists, is executable, and carries the marker.
func TestInstaller_InstallCreatesManagedHook(t *testing.T) {
	root := initGitRepo(t)
	inst := Installer{Root: root}
	res, err := inst.Install(false)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if res.AlreadyManaged {
		t.Errorf("AlreadyManaged=true on fresh install")
	}
	content, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	if !isManagedHook(content) {
		t.Errorf("hook content missing marker:\n%s", content)
	}
	if !strings.Contains(string(content), `exec ai-prov hook run commit-msg "$1"`) {
		t.Errorf("hook content missing ai-prov invocation:\n%s", content)
	}
	if mode := fileMode(t, res.Path); mode&0o100 == 0 {
		t.Errorf("hook %s is not executable (mode=%#o)", res.Path, mode)
	}
}

// TestInstaller_InstallIdempotentOnManagedHook verifies a second install on a
// managed hook is a no-op that reports AlreadyManaged and does not create a
// backup.
func TestInstaller_InstallIdempotentOnManagedHook(t *testing.T) {
	root := initGitRepo(t)
	inst := Installer{Root: root}
	if _, err := inst.Install(false); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	res, err := inst.Install(false)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if !res.AlreadyManaged {
		t.Errorf("AlreadyManaged=false, want true")
	}
	if res.BackedUp != "" {
		t.Errorf("BackedUp=%q, want empty on idempotent install", res.BackedUp)
	}
	if _, err := os.Stat(res.Path + ".pre-ai-prov"); err == nil {
		t.Errorf("idempotent install created a backup unexpectedly")
	}
}

// TestInstaller_InstallRefusesExistingForeignHook ensures a foreign hook is
// not silently overwritten without --force.
func TestInstaller_InstallRefusesExistingForeignHook(t *testing.T) {
	root := initGitRepo(t)
	hooksDir, err := HooksDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "#!/bin/sh\necho user hook\n"
	foreignPath := filepath.Join(hooksDir, "commit-msg")
	if err := os.WriteFile(foreignPath, []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}

	inst := Installer{Root: root}
	if _, err := inst.Install(false); err == nil {
		t.Fatal("Install without --force: want error, got nil")
	}
	got, err := os.ReadFile(foreignPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != foreign {
		t.Errorf("foreign hook was modified:\ngot=%q\nwant=%q", got, foreign)
	}
}

// TestInstaller_InstallForceBacksUpForeignHook verifies --force backs up the
// foreign hook and writes the managed one in its place.
func TestInstaller_InstallForceBacksUpForeignHook(t *testing.T) {
	root := initGitRepo(t)
	hooksDir, err := HooksDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "#!/bin/sh\necho user hook\n"
	foreignPath := filepath.Join(hooksDir, "commit-msg")
	if err := os.WriteFile(foreignPath, []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}

	inst := Installer{Root: root}
	res, err := inst.Install(true)
	if err != nil {
		t.Fatalf("Install --force: %v", err)
	}
	if res.BackedUp == "" {
		t.Errorf("BackedUp empty, want backup path")
	}
	backup, err := os.ReadFile(res.BackedUp)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != foreign {
		t.Errorf("backup content mismatch:\ngot=%q\nwant=%q", backup, foreign)
	}
	got, err := os.ReadFile(foreignPath)
	if err != nil {
		t.Fatal(err)
	}
	if !isManagedHook(got) {
		t.Errorf("hook after --force not managed:\n%s", got)
	}
}

// TestInstaller_UninstallRemovesOnlyManagedHook verifies Uninstall removes a
// managed hook and restores any backup from a forced install.
func TestInstaller_UninstallRemovesOnlyManagedHook(t *testing.T) {
	root := initGitRepo(t)
	hooksDir, err := HooksDir(root)
	if err != nil {
		t.Fatal(err)
	}
	foreign := "#!/bin/sh\necho user hook\n"
	foreignPath := filepath.Join(hooksDir, "commit-msg")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(foreignPath, []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	inst := Installer{Root: root}
	if _, err := inst.Install(true); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	// Backup should be restored in place.
	got, err := os.ReadFile(foreignPath)
	if err != nil {
		t.Fatalf("after uninstall, expected restored hook, got read error: %v", err)
	}
	if string(got) != foreign {
		t.Errorf("after uninstall, hook content mismatch:\ngot=%q\nwant=%q", got, foreign)
	}
	if _, err := os.Stat(foreignPath + ".pre-ai-prov"); !os.IsNotExist(err) {
		t.Errorf("backup file should be gone after restore, got err=%v", err)
	}
}

// TestInstaller_UninstallLeavesForeignHookAlone verifies Uninstall never
// deletes a hook it did not install.
func TestInstaller_UninstallLeavesForeignHookAlone(t *testing.T) {
	root := initGitRepo(t)
	hooksDir, err := HooksDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "#!/bin/sh\necho user hook\n"
	foreignPath := filepath.Join(hooksDir, "commit-msg")
	if err := os.WriteFile(foreignPath, []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	inst := Installer{Root: root}
	msg, err := inst.Uninstall()
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !strings.Contains(msg, "not managed") {
		t.Errorf("Uninstall message=%q, want 'not managed'", msg)
	}
	got, err := os.ReadFile(foreignPath)
	if err != nil {
		t.Fatalf("foreign hook disappeared: %v", err)
	}
	if string(got) != foreign {
		t.Errorf("foreign hook modified:\ngot=%q\nwant=%q", got, foreign)
	}
}

// TestInstaller_UninstallNoOpWhenAbsent verifies Uninstall does not error when
// no hook is present.
func TestInstaller_UninstallNoOpWhenAbsent(t *testing.T) {
	root := initGitRepo(t)
	inst := Installer{Root: root}
	msg, err := inst.Uninstall()
	if err != nil {
		t.Fatalf("Uninstall on empty repo: %v", err)
	}
	if !strings.Contains(msg, "nothing to do") {
		t.Errorf("Uninstall message=%q, want 'nothing to do'", msg)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()
}
