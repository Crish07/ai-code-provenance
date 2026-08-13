// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

package install

import "testing"

func TestShellFragment_PreservesForeignContentAndIsIdempotent(t *testing.T) {
	first, err := ShellFragment("export FOO=bar\n", "/home/a/.local/bin")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ShellFragment(first, "/home/a/.local/bin")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("fragment not idempotent:\n%s", second)
	}
	if want := "export FOO=bar\n"; first[:len(want)] != want {
		t.Fatalf("foreign content changed: %q", first)
	}
	if _, err := ShellFragment(ShellBeginMark, "/home/a/.local/bin"); err == nil {
		t.Fatal("malformed markers accepted")
	}
}

func TestWindowsUserPATH_DeduplicatesEntry(t *testing.T) {
	got, err := WindowsUserPATH("C:/Tools;C:/Users/a/AppData/Local/Programs/ai-prov;c:/users/a/appdata/local/programs/ai-prov", "C:/Users/a/AppData/Local/Programs/ai-prov")
	if err != nil {
		t.Fatal(err)
	}
	if want := "C:/Users/a/AppData/Local/Programs/ai-prov;C:/Tools"; got != want {
		t.Fatalf("PATH=%q want %q", got, want)
	}
}
