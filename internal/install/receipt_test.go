// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultLayout_PlatformPaths(t *testing.T) {
	tests := []struct {
		name, goos, home, localAppData, root, receipt, extension string
	}{
		{"darwin", "darwin", "/Users/a", "", "/Users/a/.local/bin", "/Users/a/.local/share/ai-prov/install-receipt.json", ""},
		{"linux", "linux", "/home/a", "", "/home/a/.local/bin", "/home/a/.local/share/ai-prov/install-receipt.json", ""},
		{"windows", "windows", "C:/Users/a", "C:/Users/a/AppData/Local", "C:/Users/a/AppData/Local/Programs/ai-prov", "C:/Users/a/AppData/Local/ai-prov/install-receipt.json", ".exe"},
		{"windows backslashes", "windows", `C:\Users\a`, `C:\Users\a\AppData\Local`, "C:/Users/a/AppData/Local/Programs/ai-prov", "C:/Users/a/AppData/Local/ai-prov/install-receipt.json", ".exe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DefaultLayout(Environment{GOOS: tt.goos, Home: tt.home, LocalAppData: tt.localAppData})
			if err != nil {
				t.Fatalf("DefaultLayout: %v", err)
			}
			if got.InstallRoot != tt.root || got.ReceiptPath != tt.receipt {
				t.Fatalf("layout = %#v", got)
			}
			if got.Binaries[0].Path != tt.root+"/ai-prov"+tt.extension || got.Binaries[1].Path != tt.root+"/ai-prov-mcp"+tt.extension {
				t.Fatalf("binaries = %#v", got.Binaries)
			}
		})
	}
}

func TestDefaultLayout_RejectsIncompleteOrUnsupportedEnvironment(t *testing.T) {
	for _, env := range []Environment{{GOOS: "linux"}, {GOOS: "windows", Home: "C:/Users/a"}, {GOOS: "plan9", Home: "/home/a"}} {
		if _, err := DefaultLayout(env); err == nil {
			t.Fatalf("DefaultLayout(%#v) succeeded", env)
		}
	}
}

func TestInstall_WindowsBackslashEnvironmentAndOverrideAreCanonicalized(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "release")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ai-prov.exe", "ai-prov-mcp.exe"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Install(Options{
		Environment: Environment{GOOS: "windows", Home: `C:\Users\a`, LocalAppData: `C:\Users\a\AppData\Local`},
		SourceDir:   source,
		InstallRoot: `C:\Users\a\bin\ai-prov`,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.Layout.InstallRoot != "C:/Users/a/bin/ai-prov" || result.Layout.ReceiptPath != "C:/Users/a/AppData/Local/ai-prov/install-receipt.json" {
		t.Fatalf("layout = %#v", result.Layout)
	}
}

func TestReceipt_Validate(t *testing.T) {
	good := Receipt{SchemaVersion: ReceiptSchemaVersion, Platform: "linux", InstallRoot: "/home/a/.local/bin", Binaries: []InstalledBin{
		{Name: "ai-prov", Path: "/home/a/.local/bin/ai-prov", SHA256: hash()},
		{Name: "ai-prov-mcp", Path: "/home/a/.local/bin/ai-prov-mcp", SHA256: hash()},
	}, PATH: PATHRecord{Method: "shell_fragment", Entry: "/home/a/.local/bin", BeginMark: "# >>> ai-prov >>>", EndMark: "# <<< ai-prov <<<"}}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid receipt rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Receipt){
		"unknown version":   func(r *Receipt) { r.SchemaVersion++ },
		"escape root":       func(r *Receipt) { r.Binaries[0].Path = "/tmp/ai-prov" },
		"traversal":         func(r *Receipt) { r.Binaries[0].Path = "/home/a/.local/bin/../evil" },
		"invalid hash":      func(r *Receipt) { r.Binaries[0].SHA256 = "abc" },
		"path outside root": func(r *Receipt) { r.PATH.Entry = "/tmp" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := good
			bad.Binaries = append([]InstalledBin(nil), good.Binaries...)
			mutate(&bad)
			if err := bad.Validate(); err == nil {
				t.Fatal("Validate succeeded")
			}
		})
	}
}

func hash() string { return "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" }

func TestInstall_DryRunAndAtomicCopy(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "release")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ai-prov", "ai-prov-mcp"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(name+" binary"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	installRoot := filepath.Join(root, "bin")
	opts := Options{Environment: Environment{GOOS: "linux", Home: root}, SourceDir: source, InstallRoot: installRoot, DryRun: true}
	result, err := Install(opts)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installRoot, "ai-prov")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote binary: %v", err)
	}
	if result.Receipt.PATH.Method != "pending" {
		t.Fatalf("PATH method=%q", result.Receipt.PATH.Method)
	}

	opts.DryRun = false
	if _, err := Install(opts); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(installRoot, "ai-prov"))
	if err != nil || string(got) != "ai-prov binary" {
		t.Fatalf("installed binary=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".local", "share", "ai-prov", "install-receipt.json")); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if _, err := Install(opts); err != nil {
		t.Fatalf("idempotent install: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "ai-prov"), []byte("other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(opts); err == nil {
		t.Fatal("replacement without force succeeded")
	}
	opts.Force = true
	if _, err := Install(opts); err != nil {
		t.Fatalf("forced replacement: %v", err)
	}
}

func TestInstall_MissingPairedBinaryLeavesNoInstall(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "release")
	if err := os.Mkdir(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "ai-prov"), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	installRoot := filepath.Join(root, "bin")
	_, err := Install(Options{Environment: Environment{GOOS: "linux", Home: root}, SourceDir: source, InstallRoot: installRoot})
	if err == nil {
		t.Fatal("Install succeeded without ai-prov-mcp")
	}
	if _, statErr := os.Stat(installRoot); !os.IsNotExist(statErr) {
		t.Fatalf("install root exists after validation failure: %v", statErr)
	}
}

func TestUninstall_OnlyRemovesMatchingReceiptFiles(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(bin, "ai-prov"), filepath.Join(bin, "ai-prov-mcp")}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte(p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h1, _ := fileHash(paths[0])
	h2, _ := fileHash(paths[1])
	receipt := Receipt{SchemaVersion: 1, Platform: "linux", InstallRoot: filepath.ToSlash(bin), Binaries: []InstalledBin{{Name: "ai-prov", Path: filepath.ToSlash(paths[0]), SHA256: h1}, {Name: "ai-prov-mcp", Path: filepath.ToSlash(paths[1]), SHA256: h2}}, PATH: PATHRecord{Method: "none", Entry: filepath.ToSlash(bin)}}
	if err := os.WriteFile(paths[1], []byte("changed"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := Uninstall(receipt, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Removed) != 1 || len(result.Preserved) != 1 {
		t.Fatalf("result=%#v", result)
	}
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("matching file remains: %v", err)
	}
	if _, err := os.Stat(paths[1]); err != nil {
		t.Fatalf("changed file removed: %v", err)
	}
}
