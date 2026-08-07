package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"ai-prov/internal/workspace"
)

type File struct{ Path, Hash string }
type Manifest struct {
	ID           string
	Files        []File
	SkippedCount int
}

func Create(root, id string, max int64) (Manifest, error) {
	files, skipped, err := workspace.Scan(root, max)
	if err != nil {
		return Manifest{}, err
	}
	dir := filepath.Join(root, ".ai-provenance", "snapshots", id)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return Manifest{}, err
	}
	m := Manifest{ID: id, SkippedCount: len(skipped)}
	for _, f := range files {
		data := Normalize(f.Data)
		m.Files = append(m.Files, File{f.Path, Hash(data)})
		p := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err = os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return Manifest{}, err
		}
		if err = os.WriteFile(p, data, 0o644); err != nil {
			return Manifest{}, err
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return Manifest{}, err
	}
	tmp, err := os.CreateTemp(dir, ".manifest-*")
	if err != nil {
		return Manifest{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		return Manifest{}, err
	}
	if err = tmp.Close(); err != nil {
		return Manifest{}, err
	}
	if err = os.Rename(tmpName, filepath.Join(dir, "manifest.json")); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// Hash returns the SHA-256 of canonical snapshot content.
func Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// Matches reports whether current canonical content is identical to the
// manifest entry. Finish uses it as a fast path before allocating a line diff.
func Matches(data []byte, expectedHash string) bool {
	return Hash(data) == expectedHash
}

// Normalize produces the canonical text representation used on both sides of
// a provenance diff. Keeping this operation shared prevents an unchanged CRLF
// file from differing from its LF-normalized snapshot at session finish.
func Normalize(data []byte) []byte {
	return []byte(strings.ReplaceAll(string(data), "\r\n", "\n"))
}

func Verify(root string, m Manifest) bool {
	for _, f := range m.Files {
		b, e := os.ReadFile(filepath.Join(root, ".ai-provenance", "snapshots", m.ID, filepath.FromSlash(f.Path)))
		if e != nil {
			return false
		}
		if !Matches(b, f.Hash) {
			return false
		}
	}
	return true
}
func Read(root, id string) (Manifest, error) {
	b, e := os.ReadFile(filepath.Join(root, ".ai-provenance", "snapshots", id, "manifest.json"))
	if e != nil {
		return Manifest{}, e
	}
	var m Manifest
	e = json.Unmarshal(b, &m)
	return m, e
}
