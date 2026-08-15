// Copyright (c) 2026 The ai-code-provenance Crish07.
//
// Licensed under the MIT License.
// See LICENSE file in the project root for full license text.

/*
 * @Description:
 * @Date: 2026-08-04 16:25:03
 * @LastEditTime: 2026-08-13 14:33:34
 */
package provenance

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestIdentities(t *testing.T) {
	v := Identities("a.go", []string{"x", "x", "y"})
	if v[0].Occurrence != 1 || v[1].Occurrence != 2 || v[0].Hash == v[1].Hash || v[0].Before != "" || v[2].After != "" {
		t.Fatalf("%#v", v)
	}
	if v[0].Hash != Identities("a.go", []string{"x", "x", "y"})[0].Hash {
		t.Fatal("unstable")
	}
}

func TestIdentities_UsesPathAndNearestNonBlankAnchors(t *testing.T) {
	lines := []string{"before", "", "target", "  ", "after"}
	got := Identities("a.go", lines)
	otherPath := Identities("b.go", lines)
	hash := func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])
	}
	if got[2].Before != hash("before") || got[2].After != hash("after") {
		t.Fatalf("anchors = %#v, want nearest non-blank neighbors", got[2])
	}
	if got[2].Hash == otherPath[2].Hash {
		t.Fatal("identity hash must include file path")
	}
}

func TestIdentities_NormalizesCRLF(t *testing.T) {
	lf := Identities("a.go", []string{"one", "two"})
	crlf := Identities("a.go", []string{"one\r", "two\r"})
	if lf[0] != crlf[0] || lf[1] != crlf[1] {
		t.Fatalf("LF %#v and CRLF %#v identities differ", lf, crlf)
	}
}

func TestContentHash_NormalizesCRLF(t *testing.T) {
	if ContentHash("line") != ContentHash("line\r") {
		t.Fatal("line content hash must normalize CRLF")
	}
}
